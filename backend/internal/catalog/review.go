package catalog

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// ReviewAudit is the caller-owned identity carried into the catalog outbox.
// Delivery to the platform audit chain happens after the catalog transaction.
type ReviewAudit struct {
	Time      time.Time
	ActorID   int64
	ActorRole string
	IP        string
	RequestID string
}

// ReviewAuditDetail is the durable, non-secret decision payload.
func ReviewAuditDetail(decision ProductStatus, reason string) ([]byte, error) {
	return json.Marshal(struct {
		Decision ProductStatus `json:"decision"`
		Reason   string        `json:"reason"`
	}{Decision: decision, Reason: strings.TrimSpace(reason)})
}

// ReviewStore is the catalog-side boundary for platform product moderation.
// The concrete store must perform the conditional status update and audit it
// in one transaction so a concurrent decision cannot be applied twice.
type ReviewStore interface {
	ListProducts(context.Context, ProductFilter, Plan) (ProductPage, error)
	ReviewProduct(context.Context, string, ProductStatus, string, ReviewAudit) (Product, error)
}

// ReviewService contains only review-domain validation. Authentication,
// permission checks, and actor identity stay at the API/platform boundary.
type ReviewService struct {
	store ReviewStore
}

// ValidateReviewDecision checks the state machine before a store mutation.
// The concrete store repeats the check after locking the row.
func ValidateReviewDecision(current, target ProductStatus, reason string) error {
	if current != StatusReview || (target != StatusOn && target != StatusRejected) {
		return ErrInvalidReviewTransition
	}
	if target == StatusRejected && strings.TrimSpace(reason) == "" {
		return ErrReviewReasonRequired
	}
	return nil
}

func NewReviewService(store ReviewStore) *ReviewService {
	return &ReviewService{store: store}
}

// Pending returns only products waiting for a platform decision.
func (s *ReviewService) Pending(ctx context.Context, filter ProductFilter, plan Plan) (ProductPage, error) {
	// The review queue is always server-scoped to products awaiting review;
	// callers may only narrow it by merchant and submission time.
	filter.Status = StatusReview
	return s.store.ListProducts(ctx, filter, plan)
}

func (s *ReviewService) Approve(ctx context.Context, publicID string, audit ReviewAudit) (Product, error) {
	return s.store.ReviewProduct(ctx, publicID, StatusOn, "", audit)
}

func (s *ReviewService) Reject(ctx context.Context, publicID, reason string, audit ReviewAudit) (Product, error) {
	reason = strings.TrimSpace(reason)
	if err := ValidateReviewDecision(StatusReview, StatusRejected, reason); err != nil {
		return Product{}, err
	}
	return s.store.ReviewProduct(ctx, publicID, StatusRejected, reason, audit)
}
