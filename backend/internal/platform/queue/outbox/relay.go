// Package outbox relays committed ledger events to the queue.
package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/hibiken/asynq"
	"github.com/leishen8806/Dr.life-/backend/internal/ledger"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/queue"
)

const (
	DefaultBatchSize = 100
	unknownTaskType  = "ops:outbox_unknown:v1"
)

type ledgerOutbox interface {
	ClaimOutbox(context.Context, int) (*ledger.OutboxBatch, error)
}

// Route maps a ledger outbox topic to its registered asynq task type.
type Route struct{ TaskType string }

type Options struct {
	BatchSize int
	Routes    map[string]Route
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
		payload, err := json.Marshal(struct {
			OutboxID int64 `json:"outbox_id"`
		}{OutboxID: event.ID})
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

func (r *Relay) route(topic string) (string, []asynq.Option) {
	if route, ok := r.options.Routes[topic]; ok && route.TaskType != "" {
		return route.TaskType, nil
	}
	// Unknown topics are isolated by Worker as an unregistered task and archived
	// immediately because MaxRetry is zero; they cannot loop indefinitely.
	return unknownTaskType, []asynq.Option{asynq.Queue(queue.QueueDefault), asynq.MaxRetry(0)}
}

// Run polls continuously, using bounded idle backoff while the outbox is empty.
func (r *Relay) Run(ctx context.Context) error {
	delay := 10 * time.Millisecond
	for {
		count, err := r.RunOnce(ctx)
		if err != nil {
			return err
		}
		if count > 0 {
			delay = 10 * time.Millisecond
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
			if delay < 2*time.Second {
				delay *= 2
			}
		}
	}
}
