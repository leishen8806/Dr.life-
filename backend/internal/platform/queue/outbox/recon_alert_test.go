package outbox_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/leishen8806/Dr.life-/backend/internal/comms/telegram"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/opsalert"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/queue/outbox"
)

func TestReconciliationAlertHandlerSendsSanitizedAlertOnce(t *testing.T) {
	sender := &alertSender{}
	handlers := opsalert.NewReconciliationHandlers(opsalert.Options{
		Sender: sender, BotID: "ops-alert", ChatID: -1001, Environment: "test", Language: "en",
		MergeWindow: time.Minute,
	})
	task := asynq.NewTask(outbox.TaskReconFailed, []byte(`{"outbox_id":41,"alert":{"run_id":7,"day":"2026-10-27","counts":{"BALANCE_CACHE":2},"account_ids":[9988],"amount":12345}}`))
	if err := handlers.Alert(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if err := handlers.Alert(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if err := handlers.Flush(context.Background(), flushTask(outbox.TaskReconFailed)); err != nil {
		t.Fatal(err)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("sent=%d, want one", len(sender.messages))
	}
	message := sender.messages[0]
	for _, forbidden := range []string{"9988", "12345", "account_ids", "amount"} {
		if strings.Contains(message, forbidden) {
			t.Fatalf("alert leaks %q: %s", forbidden, message)
		}
	}
	for _, required := range []string{"test", "reconciliation failed", "2026-10-27", "run 7", "issues 2", "outbox 41"} {
		if !strings.Contains(message, required) {
			t.Fatalf("alert missing %q: %s", required, message)
		}
	}
}

func TestReconciliationAlertHandlerClassifiesTelegramErrors(t *testing.T) {
	for _, tc := range []struct {
		name     string
		result   telegram.Result
		wantSkip bool
	}{
		{name: "rate limited", result: telegram.Result{Kind: telegram.ResultRateLimited}},
		{name: "temporary", result: telegram.Result{Kind: telegram.ResultTemporaryError}},
		{name: "blocked", result: telegram.Result{Kind: telegram.ResultUserBlocked}, wantSkip: true},
		{name: "chat not found", result: telegram.Result{Kind: telegram.ResultChatNotFound}, wantSkip: true},
		{name: "permanent", result: telegram.Result{Kind: telegram.ResultPermanentError}, wantSkip: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sender := &alertSender{result: tc.result}
			handlers := opsalert.NewReconciliationHandlers(opsalert.Options{Sender: sender, BotID: "ops-alert", ChatID: -1001})
			if err := handlers.Alert(context.Background(), asynq.NewTask(outbox.TaskReconStale, []byte(`{"outbox_id":42,"alert":{"run_id":8,"day":"2026-10-27"}}`))); err != nil {
				t.Fatal(err)
			}
			err := handlers.Flush(context.Background(), flushTask(outbox.TaskReconStale))
			if (err != nil && strings.Contains(err.Error(), asynq.SkipRetry.Error())) != tc.wantSkip {
				t.Fatalf("err=%v, wantSkip=%v", err, tc.wantSkip)
			}
			if !tc.wantSkip && err == nil {
				t.Fatal("retryable Telegram result returned nil")
			}
		})
	}
}

func TestReconciliationAlertHandlerWithoutConfigurationDoesNotSend(t *testing.T) {
	sender := &alertSender{}
	metrics := &alertMetrics{}
	handlers := opsalert.NewReconciliationHandlers(opsalert.Options{Sender: sender, Metrics: metrics})
	if err := handlers.Alert(context.Background(), asynq.NewTask(outbox.TaskReconFailed, []byte(`{"outbox_id":43,"alert":{"run_id":9}}`))); err != nil {
		t.Fatal(err)
	}
	if err := handlers.Flush(context.Background(), flushTask(outbox.TaskReconFailed)); err != nil {
		t.Fatal(err)
	}
	if len(sender.messages) != 0 || metrics.undeliverable != 1 {
		t.Fatalf("sent=%d undeliverable=%d, want 0/1", len(sender.messages), metrics.undeliverable)
	}
}

func flushTask(topic string) *asynq.Task {
	return asynq.NewTask(opsalert.TaskFlush, []byte(`{"topic":"`+topic+`"}`))
}

type alertSender struct {
	result   telegram.Result
	messages []string
}

type alertMetrics struct{ undeliverable int }

func (m *alertMetrics) OpsAlertUndeliverable(string) { m.undeliverable++ }

func (s *alertSender) SendMessage(_ context.Context, _ string, _ int64, text string, _ telegram.SendOptions) telegram.Result {
	s.messages = append(s.messages, text)
	return s.result
}
