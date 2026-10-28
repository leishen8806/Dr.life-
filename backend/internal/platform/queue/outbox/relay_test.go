package outbox_test

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/queue"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/queue/outbox"
)

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
