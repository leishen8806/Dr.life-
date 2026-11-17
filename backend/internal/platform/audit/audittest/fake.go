package audittest

import (
	"context"
	"sync"

	platformaudit "github.com/leishen8806/Dr.life-/backend/internal/platform/audit"
)

type Writer struct {
	mu      sync.Mutex
	Records []platformaudit.Entry
	Err     error
	seen    map[string]struct{}
}

func (w *Writer) Append(_ context.Context, entry platformaudit.Entry) (platformaudit.Record, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.Err != nil {
		return platformaudit.Record{}, w.Err
	}
	w.Records = append(w.Records, entry)
	return platformaudit.Record{}, nil
}

func (w *Writer) AppendOnce(ctx context.Context, eventID string, entry platformaudit.Entry) (platformaudit.Record, error) {
	w.mu.Lock()
	if _, ok := w.seen[eventID]; ok {
		w.mu.Unlock()
		return platformaudit.Record{}, nil
	}
	w.mu.Unlock()
	record, err := w.Append(ctx, entry)
	if err != nil {
		return record, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.seen == nil {
		w.seen = make(map[string]struct{})
	}
	w.seen[eventID] = struct{}{}
	return record, nil
}

type AnchorSink struct {
	mu      sync.Mutex
	Records []platformaudit.AnchorRecord
	Err     error
}

func (s *AnchorSink) Put(_ context.Context, record platformaudit.AnchorRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Records = append(s.Records, record)
	return s.Err
}
