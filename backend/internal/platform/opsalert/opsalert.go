// Package opsalert owns operations-only alert formatting and delivery controls.
package opsalert

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/hibiken/asynq"
	"github.com/leishen8806/Dr.life-/backend/internal/comms/telegram"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/queue"
	"github.com/redis/go-redis/v9"
)

type Metrics interface{ OpsAlertUndeliverable(string) }

type Deduper interface {
	Seen(context.Context, int64) (bool, error)
	Remember(context.Context, int64) error
}

// Scheduler defers one merged delivery without coupling this package to a queue
// implementation. Production uses the existing asynq client.
type Scheduler interface {
	EnqueueContext(context.Context, *asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error)
}

type Merger interface {
	Add(context.Context, string, payload) (leader bool, error error)
	Peek(context.Context, string) (mergedPayload, bool, error)
	Discard(context.Context, string) error
}

type Options struct {
	Sender      telegram.Sender
	BotID       string
	ChatID      int64
	Environment string
	Language    string
	MergeWindow time.Duration
	Metrics     Metrics
	Logger      *slog.Logger
	Deduper     Deduper
	Merger      Merger
	Scheduler   Scheduler
}

type payload struct {
	OutboxID int64 `json:"outbox_id"`
	Alert    struct {
		RunID  int64          `json:"run_id"`
		Day    string         `json:"day"`
		Counts map[string]int `json:"counts"`
	} `json:"alert"`
}

type handler struct {
	options Options
	merger  Merger
}
type memoryDeduper struct {
	mu   sync.Mutex
	seen map[int64]struct{}
}

type mergedPayload struct {
	Payload payload
	IDs     map[int64]struct{}
}

type memoryMerger struct {
	mu     sync.Mutex
	groups map[string]mergedPayload
}

func (m *memoryMerger) Add(_ context.Context, topic string, p payload) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	group, ok := m.groups[topic]
	if !ok {
		m.groups[topic] = mergedPayload{Payload: p, IDs: map[int64]struct{}{p.OutboxID: {}}}
		return true, nil
	}
	group.IDs[p.OutboxID] = struct{}{}
	m.groups[topic] = group
	return false, nil
}

func (m *memoryMerger) Peek(_ context.Context, topic string) (mergedPayload, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	group, ok := m.groups[topic]
	return group, ok, nil
}

func (m *memoryMerger) Discard(_ context.Context, topic string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.groups, topic)
	return nil
}

func (d *memoryDeduper) Seen(_ context.Context, id int64) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.seen[id]
	return ok, nil
}
func (d *memoryDeduper) Remember(_ context.Context, id int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.seen[id] = struct{}{}
	return nil
}

type redisDeduper struct{ client redis.Cmdable }

func NewRedisDeduper(client redis.Cmdable) Deduper {
	if client == nil {
		return nil
	}
	return redisDeduper{client}
}
func (d redisDeduper) Seen(ctx context.Context, id int64) (bool, error) {
	n, e := d.client.Exists(ctx, key(id)).Result()
	return n == 1, e
}
func (d redisDeduper) Remember(ctx context.Context, id int64) error {
	return d.client.Set(ctx, key(id), "1", 30*24*time.Hour).Err()
}
func key(id int64) string { return fmt.Sprintf("ops:recon_alert:sent:%d", id) }

func NewReconciliationHandler(options Options) queue.Handler {
	return NewReconciliationHandlers(options).Alert
}

type Handlers struct {
	Alert queue.Handler
	Flush queue.Handler
}

const TaskFlush = "ops:recon_alert_flush:v1"

func NewReconciliationHandlers(options Options) Handlers {
	if options.MergeWindow <= 0 {
		options.MergeWindow = 5 * time.Minute
	}
	if options.Deduper == nil {
		options.Deduper = &memoryDeduper{seen: make(map[int64]struct{})}
	}
	if options.Merger == nil {
		options.Merger = &memoryMerger{groups: make(map[string]mergedPayload)}
	}
	if options.Language == "km" {
		options.Language = "en"
		if options.Logger != nil {
			options.Logger.Warn("OPS_ALERT_LANG=km falls back to en")
		}
	}
	if options.Language != "en" {
		options.Language = "zh"
	}
	h := &handler{options: options, merger: options.Merger}
	return Handlers{Alert: h.handle, Flush: h.flush}
}

func (h *handler) handle(ctx context.Context, task *asynq.Task) error {
	var p payload
	if err := json.Unmarshal(task.Payload(), &p); err != nil || p.OutboxID <= 0 {
		return fmt.Errorf("%w: invalid operations alert payload", asynq.SkipRetry)
	}
	seen, err := h.options.Deduper.Seen(ctx, p.OutboxID)
	if err != nil {
		return errors.New("operations alert deduplication unavailable")
	}
	if seen {
		return nil
	}
	leader, err := h.merger.Add(ctx, task.Type(), p)
	if err != nil {
		return errors.New("operations alert merge unavailable")
	}
	if !leader {
		return nil
	}
	if h.options.Scheduler != nil {
		_, err = h.options.Scheduler.EnqueueContext(ctx, asynq.NewTask(TaskFlush, []byte(`{"topic":"`+task.Type()+`"}`)), asynq.Queue("critical"), asynq.ProcessIn(h.options.MergeWindow), asynq.MaxRetry(3))
		if err != nil {
			_ = h.merger.Discard(ctx, task.Type())
			return errors.New("operations alert merge scheduling unavailable")
		}
		return nil
	}
	time.AfterFunc(h.options.MergeWindow, func() { _ = h.deliver(context.Background(), task.Type()) })
	return nil
}

func (h *handler) flush(ctx context.Context, task *asynq.Task) error {
	var request struct {
		Topic string `json:"topic"`
	}
	if err := json.Unmarshal(task.Payload(), &request); err != nil || request.Topic == "" {
		return fmt.Errorf("%w: invalid operations alert flush payload", asynq.SkipRetry)
	}
	return h.deliver(ctx, request.Topic)
}

func (h *handler) deliver(ctx context.Context, topic string) error {
	merged, ok, err := h.merger.Peek(ctx, topic)
	if err != nil {
		return errors.New("operations alert merge unavailable")
	}
	if !ok {
		return nil
	}
	if h.options.Sender == nil || strings.TrimSpace(h.options.BotID) == "" || h.options.ChatID == 0 {
		_ = h.merger.Discard(ctx, topic)
		h.undeliverable("unconfigured")
		return nil
	}
	result := h.options.Sender.SendMessage(ctx, h.options.BotID, h.options.ChatID, message(h.options.Language, h.options.Environment, topic, merged.Payload, len(merged.IDs)-1), telegram.SendOptions{})
	switch result.Kind {
	case telegram.ResultSuccess:
		for id := range merged.IDs {
			if err := h.options.Deduper.Remember(ctx, id); err != nil {
				return errors.New("operations alert deduplication unavailable")
			}
		}
		if err := h.merger.Discard(ctx, topic); err != nil {
			return errors.New("operations alert merge unavailable")
		}
		return nil
	case telegram.ResultRateLimited, telegram.ResultTemporaryError:
		return errors.New("operations alert delivery retryable")
	default:
		_ = h.merger.Discard(ctx, topic)
		h.undeliverable("telegram_permanent")
		return fmt.Errorf("%w: operations alert undeliverable", asynq.SkipRetry)
	}
}
func (h *handler) undeliverable(reason string) {
	if h.options.Logger != nil {
		h.options.Logger.Error("operations reconciliation alert undeliverable", "reason", reason)
	}
	if h.options.Metrics != nil {
		h.options.Metrics.OpsAlertUndeliverable(reason)
	}
}

var templates = map[string]map[string]string{
	"zh": {"failed": "[%s] 对账失败：日期 %s，运行 %d，差异 %d，outbox %d", "stale": "[%s] 对账卡死：日期 %s，运行 %d，outbox %d"},
	"en": {"failed": "[%s] reconciliation failed: day %s, run %d, issues %d, outbox %d", "stale": "[%s] reconciliation stale run: day %s, run %d, outbox %d"},
}

func message(lang, env, topic string, p payload, merged int) string {
	if strings.TrimSpace(env) == "" {
		env = "unknown"
	}
	if topic == "ledger:recon_stale:v1" {
		return withMerged(fmt.Sprintf(templates[lang]["stale"], env, p.Alert.Day, p.Alert.RunID, p.OutboxID), merged, lang)
	}
	total := 0
	for _, n := range p.Alert.Counts {
		if n > 0 {
			total += n
		}
	}
	return withMerged(fmt.Sprintf(templates[lang]["failed"], env, p.Alert.Day, p.Alert.RunID, total, p.OutboxID), merged, lang)
}

func withMerged(text string, merged int, lang string) string {
	if merged <= 0 {
		return text
	}
	if lang == "zh" {
		return fmt.Sprintf("%s，+%d 条同类告警", text, merged)
	}
	return fmt.Sprintf("%s, +%d additional alert", text, merged)
}
