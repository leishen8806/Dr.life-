package trade

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/queue"
)

const TaskReservationActions = "trade:reservation_actions:v1"

type reservationProcessor interface {
	ProcessReservationActions(context.Context, int) error
}

type reservationPayload struct {
	Window string `json:"window"`
}

func RegisterReservationTask(registry *queue.Registry) error {
	return registry.Register(queue.TypeSpec{Name: TaskReservationActions, Queue: queue.QueueCritical, Timeout: 2 * time.Minute, MaxRetry: 5})
}

func ReservationScheduledTask() queue.ScheduledTask {
	return queue.ScheduledTask{Name: "trade-reservation-actions", Cron: "* * * * *", Type: TaskReservationActions, Payload: func(window time.Time) []byte {
		payload, _ := json.Marshal(reservationPayload{Window: window.UTC().Format(time.RFC3339)})
		return payload
	}}
}

func NewReservationHandler(processor reservationProcessor) queue.Handler {
	return func(ctx context.Context, task *asynq.Task) error {
		var payload reservationPayload
		if err := json.Unmarshal(task.Payload(), &payload); err != nil || payload.Window == "" {
			return fmt.Errorf("%w: invalid reservation maintenance payload", asynq.SkipRetry)
		}
		if _, err := time.Parse(time.RFC3339, payload.Window); err != nil {
			return fmt.Errorf("%w: invalid reservation maintenance window", asynq.SkipRetry)
		}
		if processor == nil {
			return fmt.Errorf("%w: reservation processor unavailable", asynq.SkipRetry)
		}
		return processor.ProcessReservationActions(ctx, 100)
	}
}
