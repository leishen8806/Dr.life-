package outbox_test

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/leishen8806/Dr.life-/backend/internal/ledger"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/queue"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/queue/outbox"
)

type failingLedgerOutbox struct {
	errs  []error
	calls int
}

func (f *failingLedgerOutbox) ClaimOutbox(context.Context, int) (*ledger.OutboxBatch, error) {
	f.calls++
	if len(f.errs) == 0 {
		return nil, context.Canceled
	}
	err := f.errs[0]
	f.errs = f.errs[1:]
	return nil, err
}

func TestRelayRunRetriesTransientDatabaseFailureBeforeRetryingClaim(t *testing.T) {
	ledgerOutbox := &failingLedgerOutbox{errs: []error{
		&pgconn.PgError{Code: "40001"},
		context.Canceled,
	}}
	retries := 0
	relay, err := outbox.New(ledgerOutbox, discardQueueClient{}, outbox.Options{
		RetryInitialDelay: time.Nanosecond,
		RetryMaxDelay:     time.Nanosecond,
		OnRetry: func(error) {
			retries++
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := relay.Run(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error=%v, want context cancellation after recovered claim", err)
	}
	if ledgerOutbox.calls != 2 || retries != 1 {
		t.Fatalf("calls=%d retries=%d, want 2/1", ledgerOutbox.calls, retries)
	}
}

func TestRelayRunRetriesPostgresRestartAndConnectionErrorClasses(t *testing.T) {
	for _, code := range []string{"08000", "08001", "08003", "08004", "08006", "08007", "08P01", "57P01", "57P02", "57P03"} {
		t.Run(code, func(t *testing.T) {
			ledgerOutbox := &failingLedgerOutbox{errs: []error{&pgconn.PgError{Code: code}, context.Canceled}}
			relay, err := outbox.New(ledgerOutbox, discardQueueClient{}, outbox.Options{RetryInitialDelay: time.Nanosecond, RetryMaxDelay: time.Nanosecond})
			if err != nil {
				t.Fatal(err)
			}
			if err := relay.Run(context.Background()); !errors.Is(err, context.Canceled) {
				t.Fatalf("Run error=%v, want context cancellation after retry", err)
			}
			if ledgerOutbox.calls != 2 {
				t.Fatalf("calls=%d, want 2", ledgerOutbox.calls)
			}
		})
	}
}

func TestRelayRunKeepsRetryingAfterBackoffReachesCap(t *testing.T) {
	ledgerOutbox := &failingLedgerOutbox{errs: []error{
		&pgconn.PgError{Code: "57P01"},
		&pgconn.PgError{Code: "57P01"},
		&pgconn.PgError{Code: "57P01"},
		&pgconn.PgError{Code: "57P01"},
		context.Canceled,
	}}
	retries := 0
	relay, err := outbox.New(ledgerOutbox, discardQueueClient{}, outbox.Options{
		RetryInitialDelay: time.Nanosecond,
		RetryMaxDelay:     time.Nanosecond,
		OnRetry:           func(error) { retries++ },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := relay.Run(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error=%v, want context cancellation", err)
	}
	if ledgerOutbox.calls != 5 || retries != 4 {
		t.Fatalf("calls=%d retries=%d, want 5/4 after delay cap", ledgerOutbox.calls, retries)
	}
}

func TestRelayRunRetriesSafeAndNetworkTransportErrors(t *testing.T) {
	for _, err := range []error{safeRetryError{}, temporaryNetworkError{}} {
		ledgerOutbox := &failingLedgerOutbox{errs: []error{err, context.Canceled}}
		relay, newErr := outbox.New(ledgerOutbox, discardQueueClient{}, outbox.Options{RetryInitialDelay: time.Nanosecond, RetryMaxDelay: time.Nanosecond})
		if newErr != nil {
			t.Fatal(newErr)
		}
		if got := relay.Run(context.Background()); !errors.Is(got, context.Canceled) {
			t.Fatalf("Run error=%v, want context cancellation after transport retry", got)
		}
	}
}

type safeRetryError struct{}

func (safeRetryError) Error() string     { return "safe retry transport error" }
func (safeRetryError) SafeToRetry() bool { return true }

type temporaryNetworkError struct{}

func (temporaryNetworkError) Error() string   { return "temporary network error" }
func (temporaryNetworkError) Timeout() bool   { return false }
func (temporaryNetworkError) Temporary() bool { return true }

func TestRelayRunReturnsFatalDatabaseConfigurationError(t *testing.T) {
	ledgerOutbox := &failingLedgerOutbox{errs: []error{&pgconn.PgError{Code: "42P01"}}}
	relay, err := outbox.New(ledgerOutbox, discardQueueClient{}, outbox.Options{RetryInitialDelay: time.Nanosecond, RetryMaxDelay: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}
	if err := relay.Run(context.Background()); err == nil {
		t.Fatal("Run accepted missing ledger.outbox relation as retryable")
	}
	if ledgerOutbox.calls != 1 {
		t.Fatalf("calls=%d, want 1 for fatal error", ledgerOutbox.calls)
	}
}

type discardQueueClient struct{}

func (discardQueueClient) EnqueueContext(context.Context, *asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error) {
	return nil, nil
}

func TestRelayRequiresDependencies(t *testing.T) {
	if _, err := outbox.New(nil, nil, outbox.Options{}); err == nil {
		t.Fatal("relay accepted missing dependencies")
	}
	if outbox.DefaultBatchSize != 100 {
		t.Fatalf("batch size=%d, want 100", outbox.DefaultBatchSize)
	}
	_ = context.Background()
}

func TestLedgerRoutesCoverPublishedTopics(t *testing.T) {
	for _, topic := range ledgerPublishedTopics(t) {
		if _, ok := outbox.LedgerRoutes()[topic]; !ok {
			t.Errorf("published topic %q has no route", topic)
		}
	}
}

func ledgerPublishedTopics(t *testing.T) []string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate relay test")
	}
	ledgerDir := filepath.Join(filepath.Dir(file), "..", "..", "..", "ledger")
	packages, err := parser.ParseDir(token.NewFileSet(), ledgerDir, func(info fs.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]struct{})
	for _, pkg := range packages {
		for _, source := range pkg.Files {
			ast.Inspect(source, func(node ast.Node) bool {
				literal, ok := node.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					return true
				}
				value, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatalf("unquote %s: %v", literal.Value, err)
				}
				for _, field := range strings.FieldsFunc(value, func(r rune) bool {
					return !(r == '.' || r == '_' || r >= 'a' && r <= 'z')
				}) {
					if strings.HasPrefix(field, "ledger.") && strings.Count(field, ".") >= 2 && !strings.HasSuffix(field, ".") {
						seen[field] = struct{}{}
					}
				}
				return true
			})
		}
	}
	topics := make([]string, 0, len(seen))
	for topic := range seen {
		topics = append(topics, topic)
	}
	return topics
}

type registeringWorker struct{ handlers map[string]queue.Handler }

func (w *registeringWorker) Register(name string, handler queue.Handler) error {
	w.handlers[name] = handler
	return nil
}

func TestReconAlertRoutesReachDedicatedLandingHandlers(t *testing.T) {
	worker := &registeringWorker{handlers: make(map[string]queue.Handler)}
	if err := outbox.RegisterLedgerHandlers(worker, nil); err != nil {
		t.Fatal(err)
	}
	for topic, taskType := range map[string]string{
		"ledger.recon.failed":    outbox.TaskReconFailed,
		"ledger.recon.stale_run": outbox.TaskReconStale,
	} {
		handler := worker.handlers[taskType]
		if handler == nil {
			t.Fatalf("topic %s task %s has no registered handler", topic, taskType)
		}
		if err := handler(context.Background(), asynq.NewTask(taskType, []byte(`{"outbox_id":1}`))); err != nil {
			t.Fatalf("topic %s handler %s: %v", topic, taskType, err)
		}
		t.Logf("topic %s received by handler %s; unknownTaskType=0", topic, taskType)
	}
}
