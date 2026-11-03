// Package legalpublish provides the narrowly-scoped, approval-bound operation
// for publishing an existing legal-document draft. It never creates or edits
// legal text.
package legalpublish

import (
	"context"
	"errors"
	"strings"
)

var (
	ErrInvalidRequest            = errors.New("invalid legal publication request")
	ErrApprovalReferenceRequired = errors.New("approval reference is required")
	ErrDocumentNotDraft          = errors.New("legal document is not an unpublished draft")
)

// Executor is intentionally limited to the one write operation needed here.
type Executor interface {
	Exec(context.Context, string, ...any) (int64, error)
}

// Request names an existing draft and the approval authorizing publication.
// ApprovedBy is an active super or ops administrator ID, not an end-user account ID.
// ApprovalRef is required so the operator command cannot silently publish a draft.
type Request struct {
	Kind        string
	Version     string
	ApprovedBy  int64
	ApprovalRef string
}

// PublishDraft makes one existing, unpublished document current. Database
// triggers retain the monotonic-version and published-document immutability rules.
func PublishDraft(ctx context.Context, executor Executor, request Request) error {
	if executor == nil || (request.Kind != "privacy" && request.Kind != "terms") || request.Version == "" || request.ApprovedBy <= 0 {
		return ErrInvalidRequest
	}
	if strings.TrimSpace(request.ApprovalRef) == "" {
		return ErrApprovalReferenceRequired
	}
	rows, err := executor.Exec(ctx, `
		UPDATE ops.legal_doc
		SET published_by=$1, approval_ref=$2, published_at=clock_timestamp()
		WHERE kind=$3 AND version=$4 AND published_at IS NULL
		  AND EXISTS (
		  SELECT 1 FROM iam.admin_user
		  WHERE id=$1 AND role IN ('super', 'ops') AND status='active'
		  )`, request.ApprovedBy, strings.TrimSpace(request.ApprovalRef), request.Kind, request.Version)
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrDocumentNotDraft
	}
	return nil
}
