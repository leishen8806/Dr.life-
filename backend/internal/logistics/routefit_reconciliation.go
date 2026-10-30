package logistics

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/queue"
)

const RouteFitReconciliationTaskType = "logistics:routefit_reconciliation:v1"

// RouteFitReconciliationConfig controls the fallback scan for shipments that
// stopped receiving webhook events. The RouteFit client is supplied by R-02;
// this package deliberately owns only scheduling policy.
type RouteFitReconciliationConfig struct {
	Schedule   string
	ScanEvery  time.Duration
	NoEventFor time.Duration
}

func (c RouteFitReconciliationConfig) normalized() RouteFitReconciliationConfig {
	if c.ScanEvery <= 0 {
		c.ScanEvery = 10 * time.Minute
	}
	if c.NoEventFor <= 0 {
		c.NoEventFor = 30 * time.Minute
	}
	if c.Schedule == "" {
		c.Schedule = "*/10 * * * *"
	}
	return c
}

func (c RouteFitReconciliationConfig) Due(lastEventAt, now time.Time) bool {
	c = c.normalized()
	return !lastEventAt.IsZero() && !lastEventAt.After(now) && now.Sub(lastEventAt) >= c.NoEventFor
}

// RouteFitReconciliationRunner is implemented by the R-02 client integration.
// The receiver remains intentionally independent from RouteFit's HTTP API.
type RouteFitReconciliationRunner interface {
	ReconcileRouteFitStaleShipments(context.Context, time.Duration) error
}

// RouteFitReconciliationTaskPayload contains no shipment data or personal data.
type RouteFitReconciliationTaskPayload struct {
	Window     string `json:"window"`
	NoEventFor string `json:"no_event_for"`
}

// RegisterRouteFitReconciliationTask makes the fixed queue contract available
// to both the producer and worker.
func RegisterRouteFitReconciliationTask(registry *queue.Registry) error {
	return registry.Register(queue.TypeSpec{
		Name: RouteFitReconciliationTaskType, Queue: queue.QueueBatch,
		Timeout: 2 * time.Minute, MaxRetry: 3,
	})
}

// RouteFitReconciliationScheduledTask returns the configurable scheduled task.
func RouteFitReconciliationScheduledTask(config RouteFitReconciliationConfig) queue.ScheduledTask {
	config = config.normalized()
	return queue.ScheduledTask{
		Name: "routefit-reconciliation", Cron: config.Schedule, Type: RouteFitReconciliationTaskType,
		Payload: func(window time.Time) []byte {
			payload, _ := json.Marshal(RouteFitReconciliationTaskPayload{
				Window: window.UTC().Format(time.RFC3339), NoEventFor: config.NoEventFor.String(),
			})
			return payload
		},
	}
}

// NewRouteFitReconciliationHandler delegates retrieval to the R-02 adapter.
func NewRouteFitReconciliationHandler(runner RouteFitReconciliationRunner) queue.Handler {
	return func(ctx context.Context, task *asynq.Task) error {
		var payload RouteFitReconciliationTaskPayload
		if err := json.Unmarshal(task.Payload(), &payload); err != nil {
			return fmt.Errorf("%w: invalid RouteFit reconciliation payload", asynq.SkipRetry)
		}
		noEventFor, err := time.ParseDuration(payload.NoEventFor)
		if err != nil || noEventFor <= 0 {
			return fmt.Errorf("%w: invalid RouteFit reconciliation no-event duration", asynq.SkipRetry)
		}
		if runner == nil {
			return nil
		}
		return runner.ReconcileRouteFitStaleShipments(ctx, noEventFor)
	}
}
