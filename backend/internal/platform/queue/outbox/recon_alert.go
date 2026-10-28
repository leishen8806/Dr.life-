package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hibiken/asynq"
	"github.com/leishen8806/Dr.life-/backend/internal/comms/telegram"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/queue"
	"github.com/redis/go-redis/v9"
)

// AlertMetrics observes notifications that cannot reach the operations group.
type AlertMetrics interface{ OpsAlertUndeliverable() }

// AlertDeduper retains completed alert delivery keys. Production uses queue
// Redis; tests may use the bounded in-memory fallback.
type AlertDeduper interface {
	Seen(context.Context, int64) (bool, error)
	Remember(context.Context, int64) error
}

type ReconciliationAlertOptions struct {
	Sender      telegram.Sender
	BotID       string
	ChatID      int64
	Environment string
	MergeWindow time.Duration
	Metrics     AlertMetrics
	Logger      *slog.Logger
	Deduper     AlertDeduper
}

type alertPayload struct {
	OutboxID int64 `json:"outbox_id"`
	Alert    struct {
		RunID  int64          `json:"run_id"`
		Day    string         `json:"day"`
		Counts map[string]int `json:"counts"`
	} `json:"alert"`
}

type reconciliationAlertHandler struct {
	options ReconciliationAlertOptions
	mu      sync.Mutex
	window  map[string]time.Time
}

type memoryAlertDeduper struct {
	mu   sync.Mutex
	seen map[int64]struct{}
}

func (d *memoryAlertDeduper) Seen(_ context.Context, id int64) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.seen[id]
	return ok, nil
}

func (d *memoryAlertDeduper) Remember(_ context.Context, id int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.seen[id] = struct{}{}
	return nil
}

type redisAlertDeduper struct{ client redis.Cmdable }

// NewRedisAlertDeduper uses the queue Redis already required by the worker.
func NewRedisAlertDeduper(client redis.Cmdable) AlertDeduper {
	if client == nil {
		return nil
	}
	return redisAlertDeduper{client: client}
}

func (d redisAlertDeduper) Seen(ctx context.Context, id int64) (bool, error) {
	count, err := d.client.Exists(ctx, alertDedupKey(id)).Result()
	return count == 1, err
}

func (d redisAlertDeduper) Remember(ctx context.Context, id int64) error {
	return d.client.Set(ctx, alertDedupKey(id), "1", 30*24*time.Hour).Err()
}

func alertDedupKey(id int64) string { return fmt.Sprintf("ops:recon_alert:sent:%d", id) }

// NewReconciliationAlertHandler sends only sanitized reconciliation metadata.
// The relay keeps a stable task ID; production additionally records successful
// outbox IDs in queue Redis to cover duplicate task delivery.
func NewReconciliationAlertHandler(options ReconciliationAlertOptions) queue.Handler {
	if options.MergeWindow <= 0 {
		options.MergeWindow = 5 * time.Minute
	}
	if options.Deduper == nil {
		options.Deduper = &memoryAlertDeduper{seen: make(map[int64]struct{})}
	}
	return (&reconciliationAlertHandler{options: options, window: make(map[string]time.Time)}).handle
}

func (h *reconciliationAlertHandler) handle(ctx context.Context, task *asynq.Task) error {
	var payload alertPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil || payload.OutboxID <= 0 {
		return fmt.Errorf("%w: invalid reconciliation alert payload", asynq.SkipRetry)
	}
	seen, err := h.options.Deduper.Seen(ctx, payload.OutboxID)
	if err != nil {
		return errors.New("operations reconciliation alert deduplication unavailable")
	}
	if seen || h.windowed(task.Type()) {
		return nil
	}
	if h.options.Sender == nil || strings.TrimSpace(h.options.BotID) == "" || h.options.ChatID == 0 {
		h.logUndeliverable("operations reconciliation alert is not configured")
		h.undeliverable()
		return nil
	}
	message := reconciliationMessage(task.Type(), h.options.Environment, payload)
	result := h.options.Sender.SendMessage(ctx, h.options.BotID, h.options.ChatID, message, telegram.SendOptions{})
	switch result.Kind {
	case telegram.ResultSuccess:
		if err := h.options.Deduper.Remember(ctx, payload.OutboxID); err != nil {
			return errors.New("operations reconciliation alert deduplication unavailable")
		}
		h.rememberWindow(task.Type())
		return nil
	case telegram.ResultRateLimited, telegram.ResultTemporaryError:
		return errors.New("operations reconciliation alert delivery retryable")
	default:
		h.logUndeliverable("operations reconciliation alert is undeliverable")
		h.undeliverable()
		return fmt.Errorf("%w: operations reconciliation alert undeliverable", asynq.SkipRetry)
	}
}

func (h *reconciliationAlertHandler) logUndeliverable(message string) {
	if h.options.Logger != nil {
		h.options.Logger.Error(message)
	}
}

func (h *reconciliationAlertHandler) windowed(topic string) bool {
	now := time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()
	if until := h.window[topic]; now.Before(until) {
		return true
	}
	return false
}

func (h *reconciliationAlertHandler) rememberWindow(topic string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	h.window[topic] = now.Add(h.options.MergeWindow)
}

func (h *reconciliationAlertHandler) undeliverable() {
	if h.options.Metrics != nil {
		h.options.Metrics.OpsAlertUndeliverable()
	}
}

func reconciliationMessage(topic, environment string, payload alertPayload) string {
	environment = strings.TrimSpace(environment)
	if environment == "" {
		environment = "unknown"
	}
	if topic == TaskReconStale {
		return fmt.Sprintf("[%s] reconciliation stale run: day %s, run %d, outbox %d", environment, payload.Alert.Day, payload.Alert.RunID, payload.OutboxID)
	}
	return fmt.Sprintf("[%s] reconciliation failed: day %s, run %d, issues %d, outbox %d", environment, payload.Alert.Day, payload.Alert.RunID, issueCount(payload.Alert.Counts), payload.OutboxID)
}

func issueCount(counts map[string]int) int {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var total int
	for _, key := range keys {
		if counts[key] > 0 {
			total += counts[key]
		}
	}
	return total
}
