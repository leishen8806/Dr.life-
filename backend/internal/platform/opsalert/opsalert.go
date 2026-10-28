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
	mu      sync.Mutex
	window  map[string]time.Time
}
type memoryDeduper struct {
	mu   sync.Mutex
	seen map[int64]struct{}
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
	if options.MergeWindow <= 0 {
		options.MergeWindow = 5 * time.Minute
	}
	if options.Deduper == nil {
		options.Deduper = &memoryDeduper{seen: make(map[int64]struct{})}
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
	return (&handler{options: options, window: make(map[string]time.Time)}).handle
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
	if seen || h.windowed(task.Type()) {
		return nil
	}
	if h.options.Sender == nil || strings.TrimSpace(h.options.BotID) == "" || h.options.ChatID == 0 {
		h.undeliverable("unconfigured")
		return nil
	}
	result := h.options.Sender.SendMessage(ctx, h.options.BotID, h.options.ChatID, message(h.options.Language, h.options.Environment, task.Type(), p), telegram.SendOptions{})
	switch result.Kind {
	case telegram.ResultSuccess:
		if err := h.options.Deduper.Remember(ctx, p.OutboxID); err != nil {
			return errors.New("operations alert deduplication unavailable")
		}
		h.remember(task.Type())
		return nil
	case telegram.ResultRateLimited, telegram.ResultTemporaryError:
		return errors.New("operations alert delivery retryable")
	default:
		h.undeliverable("telegram_permanent")
		return fmt.Errorf("%w: operations alert undeliverable", asynq.SkipRetry)
	}
}
func (h *handler) windowed(topic string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return time.Now().Before(h.window[topic])
}
func (h *handler) remember(topic string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.window[topic] = time.Now().Add(h.options.MergeWindow)
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

func message(lang, env, topic string, p payload) string {
	if strings.TrimSpace(env) == "" {
		env = "unknown"
	}
	if topic == "ledger:recon_stale:v1" {
		return fmt.Sprintf(templates[lang]["stale"], env, p.Alert.Day, p.Alert.RunID, p.OutboxID)
	}
	total := 0
	for _, n := range p.Alert.Counts {
		if n > 0 {
			total += n
		}
	}
	return fmt.Sprintf(templates[lang]["failed"], env, p.Alert.Day, p.Alert.RunID, total, p.OutboxID)
}
