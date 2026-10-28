// Package outbox relays committed ledger events to the queue.
package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/leishen8806/Dr.life-/backend/internal/ledger"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/opsalert"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/queue"
)

const (
	DefaultBatchSize = 100
	unknownTaskType  = "ops:outbox_unknown:v1"
	TaskLedgerInfo   = "ledger:outbox_info:v1"
	TaskReconFailed  = "ledger:recon_failed:v1"
	TaskReconStale   = "ledger:recon_stale:v1"
)

type ledgerOutbox interface {
	ClaimOutbox(context.Context, int) (*ledger.OutboxBatch, error)
}

// Route maps a ledger outbox topic to its registered asynq task type and queue.
type Route struct {
	TaskType string
	Queue    string
}

type Options struct {
	BatchSize         int
	Routes            map[string]Route
	RetryInitialDelay time.Duration
	RetryMaxDelay     time.Duration
	OnRetry           func(error)
}

// LedgerRoutes maps every ledger-published topic to its minimal landing task.
func LedgerRoutes() map[string]Route {
	return map[string]Route{
		"ledger.txn.posted": {TaskType: TaskLedgerInfo, Queue: queue.QueueDefault}, "ledger.txn.reversed": {TaskType: TaskLedgerInfo, Queue: queue.QueueDefault},
		"ledger.txn.adjusted": {TaskType: TaskLedgerInfo, Queue: queue.QueueDefault}, "ledger.withdrawal.review_started": {TaskType: TaskLedgerInfo, Queue: queue.QueueDefault},
		"ledger.withdrawal.approved": {TaskType: TaskLedgerInfo, Queue: queue.QueueDefault}, "ledger.withdrawal.paid": {TaskType: TaskLedgerInfo, Queue: queue.QueueDefault},
		"ledger.withdrawal.failed": {TaskType: TaskLedgerInfo, Queue: queue.QueueDefault}, "ledger.withdrawal.cancelled": {TaskType: TaskLedgerInfo, Queue: queue.QueueDefault},
		"ledger.withdrawal.payee_accessed": {TaskType: TaskLedgerInfo, Queue: queue.QueueDefault},
		"ledger.payout_freeze.released":    {TaskType: TaskLedgerInfo, Queue: queue.QueueDefault}, "ledger.recon.failed": {TaskType: TaskReconFailed, Queue: queue.QueueCritical},
		"ledger.recon.stale_run": {TaskType: TaskReconStale, Queue: queue.QueueCritical},
	}
}

func RegisterLedgerTasks(registry *queue.Registry) error {
	for _, spec := range []queue.TypeSpec{
		{Name: TaskLedgerInfo, Queue: queue.QueueDefault, Timeout: time.Second * 30, MaxRetry: 3},
		{Name: TaskReconFailed, Queue: queue.QueueCritical, Timeout: time.Second * 30, MaxRetry: 3},
		{Name: TaskReconStale, Queue: queue.QueueCritical, Timeout: time.Second * 30, MaxRetry: 3},
		{Name: opsalert.TaskFlush, Queue: queue.QueueCritical, Timeout: time.Second * 30, MaxRetry: 3},
	} {
		if err := registry.Register(spec); err != nil {
			return err
		}
	}
	return nil
}

type workerRegistrar interface {
	Register(string, queue.Handler) error
}

func RegisterLedgerHandlers(worker workerRegistrar, logger *slog.Logger) error {
	return RegisterLedgerHandlersWithAlerts(worker, logger, nil)
}

// RegisterLedgerHandlersWithAlerts leaves all non-reconciliation events at the
// existing log-only landing handler. A nil alerts handler deliberately degrades
// reconciliation alerts without preventing the worker from starting.
func RegisterLedgerHandlersWithAlerts(worker workerRegistrar, logger *slog.Logger, alerts queue.Handler) error {
	for taskType, topic := range map[string]string{TaskLedgerInfo: "ledger.info", TaskReconFailed: "ledger.recon.failed", TaskReconStale: "ledger.recon.stale_run"} {
		handler := landingHandler(logger, topic)
		if alerts != nil && (taskType == TaskReconFailed || taskType == TaskReconStale) {
			handler = alerts
		}
		if err := worker.Register(taskType, handler); err != nil {
			return err
		}
	}
	return nil
}

func landingHandler(logger *slog.Logger, topic string) queue.Handler {
	return func(_ context.Context, task *asynq.Task) error {
		var payload struct {
			OutboxID int64 `json:"outbox_id"`
		}
		if err := json.Unmarshal(task.Payload(), &payload); err != nil || payload.OutboxID <= 0 {
			return fmt.Errorf("invalid outbox landing payload")
		}
		if logger != nil {
			logger.Info("ledger outbox event landed", "topic", topic, "outbox_id", payload.OutboxID)
		}
		return nil
	}
}

// Relay performs at-least-once delivery. The queue task ID is stable so a
// restart after enqueue but before MarkSent cannot create a second task.
type Relay struct {
	ledger  ledgerOutbox
	client  queue.Client
	options Options
}

func New(ledger ledgerOutbox, client queue.Client, options Options) (*Relay, error) {
	if ledger == nil || client == nil {
		return nil, errors.New("outbox relay requires ledger and queue client")
	}
	if options.BatchSize <= 0 {
		options.BatchSize = DefaultBatchSize
	}
	if options.RetryInitialDelay <= 0 {
		options.RetryInitialDelay = 100 * time.Millisecond
	}
	if options.RetryMaxDelay < options.RetryInitialDelay {
		options.RetryMaxDelay = 2 * time.Second
	}
	return &Relay{ledger: ledger, client: client, options: options}, nil
}

// RunOnce relays one locked batch. A failed enqueue releases the entire batch.
func (r *Relay) RunOnce(ctx context.Context) (int, error) {
	batch, err := r.ledger.ClaimOutbox(ctx, r.options.BatchSize)
	if err != nil {
		return 0, err
	}
	events := batch.Events()
	if len(events) == 0 {
		return 0, batch.Release(ctx)
	}
	for _, event := range events {
		taskType, options := r.route(event.Topic)
		payload, err := relayPayload(event)
		if err == nil {
			_, err = r.client.EnqueueContext(ctx, asynq.NewTask(taskType, payload), append(options, asynq.TaskID("outbox:"+strconv.FormatInt(event.ID, 10)))...)
		}
		if err != nil {
			_ = batch.Release(ctx)
			return 0, err
		}
	}
	if err := batch.MarkSent(ctx); err != nil {
		return 0, err
	}
	return len(events), nil
}

func relayPayload(event ledger.OutboxEvent) ([]byte, error) {
	payload := struct {
		OutboxID int64 `json:"outbox_id"`
		Alert    any   `json:"alert,omitempty"`
	}{OutboxID: event.ID}
	if event.Topic == "ledger.recon.failed" || event.Topic == "ledger.recon.stale_run" {
		var source struct {
			RunID  int64          `json:"run_id"`
			Day    string         `json:"day"`
			Counts map[string]int `json:"counts"`
		}
		if err := json.Unmarshal(event.Payload, &source); err != nil {
			return nil, fmt.Errorf("invalid reconciliation outbox payload: %w", err)
		}
		// Deliberately exclude account IDs, transaction IDs, amounts and timestamps
		// before the payload reaches queue Redis.
		payload.Alert = source
	}
	return json.Marshal(payload)
}

func (r *Relay) route(topic string) (string, []asynq.Option) {
	if route, ok := r.options.Routes[topic]; ok && route.TaskType != "" && route.Queue != "" {
		return route.TaskType, []asynq.Option{asynq.Queue(route.Queue)}
	}
	// Unknown topics are isolated by Worker as an unregistered task and archived
	// immediately because MaxRetry is zero; they cannot loop indefinitely.
	return unknownTaskType, []asynq.Option{asynq.Queue(queue.QueueDefault), asynq.MaxRetry(0)}
}

// Run polls continuously, using bounded idle and retry backoff. Transient
// database failures keep the worker alive; invalid database configuration is
// returned to the process supervisor as a fatal error.
func (r *Relay) Run(ctx context.Context) error {
	idleDelay := 10 * time.Millisecond
	retryDelay := r.options.RetryInitialDelay
	for {
		count, err := r.RunOnce(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if isRetryableDatabaseError(err) {
				if r.options.OnRetry != nil {
					r.options.OnRetry(err)
				}
				if !wait(ctx, retryDelay) {
					return ctx.Err()
				}
				retryDelay = nextBackoff(retryDelay, r.options.RetryMaxDelay)
				continue
			}
			return err
		}
		retryDelay = r.options.RetryInitialDelay
		if count > 0 {
			idleDelay = 10 * time.Millisecond
			continue
		}
		if !wait(ctx, idleDelay) {
			return ctx.Err()
		}
		idleDelay = nextBackoff(idleDelay, 2*time.Second)
	}
}

func isRetryableDatabaseError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if pgconn.SafeToRetry(err) {
		return true
	}
	if errors.Is(err, pgconn.ErrConnClosed) {
		return true
	}
	var connectErr *pgconn.ConnectError
	if errors.As(err, &connectErr) {
		return true
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return strings.HasPrefix(pgErr.Code, "08") || strings.HasPrefix(pgErr.Code, "57P0") || pgErr.Code == "40001" || pgErr.Code == "40P01"
	}
	var networkErr net.Error
	return errors.As(err, &networkErr) && (networkErr.Timeout() || networkErr.Temporary())
}

func wait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func nextBackoff(delay, max time.Duration) time.Duration {
	if delay >= max {
		return max
	}
	if delay > max/2 {
		return max
	}
	return delay * 2
}
