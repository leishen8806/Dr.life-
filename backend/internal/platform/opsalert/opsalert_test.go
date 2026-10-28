package opsalert

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/leishen8806/Dr.life-/backend/internal/comms/telegram"
)

func TestReconciliationHandlerMergesSameTypeWithinWindow(t *testing.T) {
	sender := &testSender{}
	handler := NewReconciliationHandler(Options{
		Sender:      sender,
		BotID:       "ops",
		ChatID:      -1001,
		Environment: "test",
		Language:    "en",
		MergeWindow: 20 * time.Millisecond,
	})
	for _, id := range []int64{51, 52} {
		task := asynq.NewTask("ledger:recon_failed:v1", []byte(`{"outbox_id":`+strconv.FormatInt(id, 10)+`,"alert":{"run_id":7,"day":"2026-10-27","counts":{"BALANCE_CACHE":2}}}`))
		if err := handler(context.Background(), task); err != nil {
			t.Fatal(err)
		}
	}

	eventually(t, time.Second, func() bool { return sender.Count() == 1 })
	if got := sender.Message(0); !strings.Contains(got, "+1 additional alert") {
		t.Fatalf("message %q does not include merged count", got)
	}
}

func TestFlushKeepsMergedAlertForRetry(t *testing.T) {
	sender := &testSender{results: []telegram.Result{{Kind: telegram.ResultTemporaryError}, {Kind: telegram.ResultSuccess}}}
	handlers := NewReconciliationHandlers(Options{Sender: sender, BotID: "ops", ChatID: -1001, Environment: "test", Language: "en"})
	if err := handlers.Alert(context.Background(), asynq.NewTask("ledger:recon_failed:v1", []byte(`{"outbox_id":61,"alert":{"run_id":7,"day":"2026-10-27"}}`))); err != nil {
		t.Fatal(err)
	}
	flush := asynq.NewTask(TaskFlush, []byte(`{"topic":"ledger:recon_failed:v1"}`))
	if err := handlers.Flush(context.Background(), flush); err == nil {
		t.Fatal("retryable Telegram error returned nil")
	}
	if err := handlers.Flush(context.Background(), flush); err != nil {
		t.Fatalf("retry flush: %v", err)
	}
	if got := sender.Count(); got != 2 {
		t.Fatalf("send attempts=%d, want 2", got)
	}
}

func TestFlushDoesNotDiscardAlertAddedDuringSend(t *testing.T) {
	sender := &blockingSender{started: make(chan struct{}), release: make(chan struct{})}
	handlers := NewReconciliationHandlers(Options{
		Sender: sender, BotID: "ops", ChatID: -1001, Environment: "test", Language: "en", MergeWindow: time.Hour,
	})
	first := asynq.NewTask("ledger:recon_failed:v1", []byte(`{"outbox_id":71,"alert":{"run_id":7,"day":"2026-10-27"}}`))
	if err := handlers.Alert(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	flush := asynq.NewTask(TaskFlush, []byte(`{"topic":"ledger:recon_failed:v1"}`))
	done := make(chan error, 1)
	go func() { done <- handlers.Flush(context.Background(), flush) }()
	<-sender.started
	second := asynq.NewTask("ledger:recon_failed:v1", []byte(`{"outbox_id":72,"alert":{"run_id":8,"day":"2026-10-28"}}`))
	if err := handlers.Alert(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	close(sender.release)
	if err := <-done; err != nil {
		t.Fatalf("first flush: %v", err)
	}
	if err := handlers.Flush(context.Background(), flush); err != nil {
		t.Fatalf("second flush: %v", err)
	}
	if got := sender.Count(); got != 2 {
		t.Fatalf("send count=%d, want 2", got)
	}
}

type testSender struct {
	mu       sync.Mutex
	messages []string
	results  []telegram.Result
}

type blockingSender struct {
	testSender
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingSender) SendMessage(ctx context.Context, botID string, chatID int64, text string, options telegram.SendOptions) telegram.Result {
	s.once.Do(func() {
		close(s.started)
		select {
		case <-s.release:
		case <-ctx.Done():
		}
	})
	return s.testSender.SendMessage(ctx, botID, chatID, text, options)
}

func (s *testSender) SendMessage(_ context.Context, _ string, _ int64, text string, _ telegram.SendOptions) telegram.Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, text)
	if len(s.results) == 0 {
		return telegram.Result{Kind: telegram.ResultSuccess}
	}
	result := s.results[0]
	s.results = s.results[1:]
	return result
}

func (s *testSender) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.messages)
}

func (s *testSender) Message(index int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.messages[index]
}

func eventually(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}
