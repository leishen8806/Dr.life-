package kess

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/queue"
)

const TaskReconcilePayments = "trade:kess_reconcile:v1"

type dueProcessor interface {
	ProcessDue(context.Context, int) error
}

type maintenancePayload struct {
	Window string `json:"window"`
}

func RegisterTask(registry *queue.Registry) error {
	return registry.Register(queue.TypeSpec{Name: TaskReconcilePayments, Queue: queue.QueueCritical, Timeout: 2 * time.Minute, MaxRetry: 5})
}

func ScheduledTask() queue.ScheduledTask {
	return queue.ScheduledTask{Name: "kess-payment-reconciliation", Cron: "* * * * *", Type: TaskReconcilePayments, Payload: func(window time.Time) []byte {
		payload, _ := json.Marshal(maintenancePayload{Window: window.UTC().Format(time.RFC3339)})
		return payload
	}}
}

func NewHandler(processor dueProcessor) queue.Handler {
	return func(ctx context.Context, task *asynq.Task) error {
		var payload maintenancePayload
		if err := json.Unmarshal(task.Payload(), &payload); err != nil || payload.Window == "" {
			return fmt.Errorf("%w: invalid KESS maintenance payload", asynq.SkipRetry)
		}
		if _, err := time.Parse(time.RFC3339, payload.Window); err != nil {
			return fmt.Errorf("%w: invalid KESS maintenance window", asynq.SkipRetry)
		}
		if processor == nil {
			return fmt.Errorf("%w: KESS maintenance processor unavailable", asynq.SkipRetry)
		}
		return processor.ProcessDue(ctx, 100)
	}
}
