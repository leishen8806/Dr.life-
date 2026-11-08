package files

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCleanupWorkerDeletesObjectBeforeWritingTerminalStatus(t *testing.T) {
	now := time.Date(2026, 10, 31, 2, 0, 0, 0, time.UTC)
	store := &cleanupMemoryStore{candidates: []cleanupCandidate{
		{ObjectKey: "objects/pending", Status: StatusPending, CreatedAt: now.Add(-25 * time.Hour)},
		{ObjectKey: "objects/retained", Status: StatusReady, RetentionUntil: ptrTime(now.Add(-time.Second))},
		{ObjectKey: "objects/unexpired", Status: StatusReady, RetentionUntil: ptrTime(now.Add(time.Second))},
	}}
	objects := &cleanupObjects{}
	worker := NewCleanupWorker(store, objects, func() time.Time { return now })

	result, err := worker.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Expired != 1 || result.Deleted != 1 || len(objects.deleted) != 2 {
		t.Fatalf("result=%+v deleted=%v", result, objects.deleted)
	}
	if got := store.terminal["objects/pending"]; got != StatusExpired {
		t.Fatalf("pending terminal status=%q, want expired", got)
	}
	if got := store.terminal["objects/retained"]; got != StatusDeleted {
		t.Fatalf("retained terminal status=%q, want deleted", got)
	}
	if _, changed := store.terminal["objects/unexpired"]; changed {
		t.Fatal("unexpired retained object was cleaned")
	}
}

func TestCleanupWorkerLeavesRecordRetryableWhenObjectDeletionFails(t *testing.T) {
	now := time.Date(2026, 10, 31, 2, 0, 0, 0, time.UTC)
	store := &cleanupMemoryStore{candidates: []cleanupCandidate{{ObjectKey: "objects/pending", Status: StatusPending, CreatedAt: now.Add(-25 * time.Hour)}}}
	worker := NewCleanupWorker(store, &cleanupObjects{err: errors.New("object store unavailable")}, func() time.Time { return now })

	if _, err := worker.Run(context.Background()); err == nil {
		t.Fatal("Cleanup succeeded after object deletion failure")
	}
	if len(store.terminal) != 0 {
		t.Fatalf("terminal updates=%v, want none", store.terminal)
	}
}

func TestRetentionDefaultIsTwoYearsAndConfigurable(t *testing.T) {
	now := time.Date(2026, 10, 31, 2, 0, 0, 0, time.UTC)
	service := New(&memoryStore{}, &fakeObjects{}, Options{Now: func() time.Time { return now }})
	if got := service.retentionUntil(); !got.Equal(now.AddDate(2, 0, 0)) {
		t.Fatalf("default retention=%s, want %s", got, now.AddDate(2, 0, 0))
	}
	service = New(&memoryStore{}, &fakeObjects{}, Options{Now: func() time.Time { return now }, RetentionPeriod: 24 * time.Hour})
	if got := service.retentionUntil(); !got.Equal(now.Add(24 * time.Hour)) {
		t.Fatalf("configured retention=%s", got)
	}
}

type cleanupMemoryStore struct {
	candidates []cleanupCandidate
	terminal   map[string]Status
}

func (s *cleanupMemoryStore) Cleanup(_ context.Context, now time.Time, deleteObject func(context.Context, string) error) (CleanupResult, error) {
	if s.terminal == nil {
		s.terminal = make(map[string]Status)
	}
	var result CleanupResult
	for _, candidate := range s.candidates {
		terminal := cleanupTerminal(candidate, now)
		if terminal == "" {
			continue
		}
		if err := deleteObject(context.Background(), candidate.ObjectKey); err != nil {
			return result, err
		}
		s.terminal[candidate.ObjectKey] = terminal
		if terminal == StatusExpired {
			result.Expired++
		} else {
			result.Deleted++
		}
	}
	return result, nil
}

type cleanupObjects struct {
	deleted []string
	err     error
}

func ptrTime(value time.Time) *time.Time { return &value }

func (s *cleanupObjects) Delete(_ context.Context, key string) error {
	s.deleted = append(s.deleted, key)
	return s.err
}
