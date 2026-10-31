package files

import (
	"context"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/queue"
)

func TestVerificationQueueRegistersAndEnqueuesFileID(t *testing.T) {
	registry := queue.NewRegistry()
	if err := RegisterTasks(registry); err != nil {
		t.Fatal(err)
	}
	client := &recordingQueueClient{}
	verifier := NewQueueVerifier(queue.NewEnqueuer(client, registry, queue.EnqueueOptions{}))
	if err := verifier.EnqueueVerification(context.Background(), File{PublicID: "file-1"}); err != nil {
		t.Fatal(err)
	}
	if client.taskType != TaskVerify || string(client.payload) != `{"public_id":"file-1"}` {
		t.Fatalf("task=%q payload=%s", client.taskType, client.payload)
	}
}

type recordingQueueClient struct {
	taskType string
	payload  []byte
}

func (c *recordingQueueClient) EnqueueContext(_ context.Context, task *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	c.taskType, c.payload = task.Type(), task.Payload()
	return &asynq.TaskInfo{}, nil
}
