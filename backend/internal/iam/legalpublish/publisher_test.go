package legalpublish

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type recordingExecutor struct {
	statement string
	args      []any
	rows      int64
	err       error
}

func (e *recordingExecutor) Exec(_ context.Context, statement string, args ...any) (int64, error) {
	e.statement = statement
	e.args = args
	return e.rows, e.err
}

func TestPublishDraftRequiresApprovalAndPublishesOnlyDraft(t *testing.T) {
	executor := &recordingExecutor{rows: 1}
	err := PublishDraft(context.Background(), executor, Request{
		Kind: "terms", Version: "v1.0", ApprovedBy: 7, ApprovalRef: "CMP-999",
	})
	if err != nil {
		t.Fatal(err)
	}
	if executor.statement == "" {
		t.Fatal("publish did not execute an update")
	}
	if !strings.Contains(executor.statement, "iam.admin_user") || !strings.Contains(executor.statement, "role IN ('super', 'ops')") || !strings.Contains(executor.statement, "status='active'") {
		t.Fatalf("publish statement does not require an active super or ops administrator: %s", executor.statement)
	}
	if len(executor.args) != 4 || executor.args[0] != int64(7) || executor.args[1] != "CMP-999" || executor.args[2] != "terms" || executor.args[3] != "v1.0" {
		t.Fatalf("args=%#v, want approved-by, approval-ref, kind, version", executor.args)
	}
	if err := PublishDraft(context.Background(), executor, Request{Kind: "terms", Version: "v1.0", ApprovedBy: 7}); !errors.Is(err, ErrApprovalReferenceRequired) {
		t.Fatalf("missing approval reference error=%v, want ErrApprovalReferenceRequired", err)
	}

	executor.rows = 0
	if err := PublishDraft(context.Background(), executor, Request{Kind: "terms", Version: "v1.0", ApprovedBy: 7, ApprovalRef: "CMP-999"}); !errors.Is(err, ErrDocumentNotDraft) {
		t.Fatalf("non-draft publish error=%v, want ErrDocumentNotDraft", err)
	}
}
