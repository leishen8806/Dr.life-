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
}

func (w *Writer) Append(_ context.Context, entry platformaudit.Entry) (platformaudit.Record, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.Records = append(w.Records, entry)
	return platformaudit.Record{}, w.Err
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
