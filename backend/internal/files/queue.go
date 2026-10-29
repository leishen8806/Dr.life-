package files

import (
	"context"
	"encoding/json"
	"time"

	"github.com/hibiken/asynq"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/queue"
)

const TaskVerify = "files:verify:v1"

type QueueVerifier struct{ enqueuer *queue.Enqueuer }

func RegisterTasks(registry *queue.Registry) error {
	return registry.Register(queue.TypeSpec{Name: TaskVerify, Queue: queue.QueueDefault, Timeout: time.Minute, MaxRetry: 5})
}

func NewQueueVerifier(enqueuer *queue.Enqueuer) *QueueVerifier {
	return &QueueVerifier{enqueuer: enqueuer}
}

func (q *QueueVerifier) EnqueueVerification(ctx context.Context, file File) error {
	if q == nil || q.enqueuer == nil || file.PublicID == "" {
		return ErrNotConfigured
	}
	payload, err := json.Marshal(struct {
		PublicID string `json:"public_id"`
	}{file.PublicID})
	if err != nil {
		return err
	}
	_, err = q.enqueuer.Enqueue(ctx, TaskVerify, payload, asynq.TaskID("file:"+file.PublicID))
	if err == asynq.ErrTaskIDConflict {
		return nil
	}
	return err
}
