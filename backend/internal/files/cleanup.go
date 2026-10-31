package files

import (
	"context"
	"time"
)

const (
	pendingUploadTTL = 24 * time.Hour
)

// CleanupResult counts terminal transitions completed only after the backing
// object was successfully removed.
type CleanupResult struct {
	Expired int
	Deleted int
}

type cleanupCandidate struct {
	PublicID       string
	ObjectKey      string
	Status         Status
	CreatedAt      time.Time
	RetentionUntil *time.Time
}

// CleanupStore owns the database transaction that holds each file row stable
// while its object is deleted, then writes the terminal status.
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
	if file.RetentionUntil != nil && !file.RetentionUntil.After(now) && (file.Status == StatusPending || file.Status == StatusReady || file.Status == StatusRejected) {
		return StatusDeleted
	}
	if file.Status == StatusPending && file.CreatedAt.Before(now.Add(-pendingUploadTTL)) {
		return StatusExpired
	}
	return ""
}
