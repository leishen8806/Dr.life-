package files

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/hibiken/asynq"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/queue"
)

const (
	pendingUploadTTL = 24 * time.Hour
	deleteTimeout    = 30 * time.Second
)

// CleanupResult counts terminal transitions completed only after the backing
// object was successfully removed.
type CleanupResult struct {
	Expired int
	Deleted int
}

type cleanupRegistrar interface {
	Register(string, queue.Handler) error
}

func RegisterCleanupHandler(worker cleanupRegistrar, store CleanupStore, objects objectDeleter) error {
	if worker == nil {
		return ErrNotConfigured
	}
	return worker.Register(TaskCleanup, NewCleanupWorker(store, objects, nil).Handle)
}

func (w *CleanupWorker) Handle(ctx context.Context, task *asynq.Task) error {
	if task == nil {
		return ErrNotConfigured
	}
	var payload struct {
		Window time.Time `json:"window"`
	}
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return errors.New("invalid file cleanup task")
	}
	_, err := w.Run(ctx)
	return err
}

type cleanupCandidate struct {
	PublicID       string
	ObjectKey      string
	Status         Status
	CreatedAt      time.Time
	RetentionUntil *time.Time
}

// CleanupStore claims each row before deleting its object and never holds a
// database row lock across object-storage I/O.
type CleanupStore interface {
	Cleanup(context.Context, time.Time, func(context.Context, string) error) (CleanupResult, error)
}

type objectDeleter interface {
	Delete(context.Context, string) error
}

type CleanupWorker struct {
	store   CleanupStore
	objects objectDeleter
	now     func() time.Time
}

// CleanupScheduledTask runs hourly; each run uses the worker clock so a
// delayed queue delivery still applies the correct retention boundary.
func CleanupScheduledTask() queue.ScheduledTask {
	return queue.ScheduledTask{
		Name: "files_cleanup", Cron: "@hourly", Type: TaskCleanup,
		Payload: func(window time.Time) []byte {
			payload, _ := json.Marshal(struct {
				Window time.Time `json:"window"`
			}{Window: window.UTC()})
			return payload
		},
	}
}

func NewCleanupWorker(store CleanupStore, objects objectDeleter, now func() time.Time) *CleanupWorker {
	if now == nil {
		now = time.Now
	}
	return &CleanupWorker{store: store, objects: objects, now: now}
}

func (w *CleanupWorker) Run(ctx context.Context) (CleanupResult, error) {
	if w == nil || w.store == nil || w.objects == nil {
		return CleanupResult{}, ErrNotConfigured
	}
	return w.store.Cleanup(ctx, w.now().UTC(), w.objects.Delete)
}

func cleanupTerminal(file cleanupCandidate, now time.Time) Status {
	if (file.Status == StatusPending || file.Status == StatusVerifying) && file.CreatedAt.Before(now.Add(-pendingUploadTTL)) {
		return StatusExpired
	}
	if file.RetentionUntil != nil && !file.RetentionUntil.After(now) && (file.Status == StatusReady || file.Status == StatusRejected) {
		return StatusDeleted
	}
	return ""
}
