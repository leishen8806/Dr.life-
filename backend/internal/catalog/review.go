package catalog

import (
	"context"
	"strings"
)

// ReviewStore is the catalog-side boundary for platform product moderation.
// The concrete store must perform the conditional status update and audit it
// in one transaction so a concurrent decision cannot be applied twice.
type ReviewStore interface {
	ListProducts(context.Context, ProductFilter, Plan) (ProductPage, error)
	ReviewProduct(context.Context, string, ProductStatus, string) (Product, error)
}

// ReviewService contains only review-domain validation. Authentication,
// permission checks, and actor identity stay at the API/platform boundary.
type ReviewService struct {
	store ReviewStore
}

func NewReviewService(store ReviewStore) *ReviewService {
	return &ReviewService{store: store}
}

// Pending returns only products waiting for a platform decision.
func (s *ReviewService) Pending(ctx context.Context, plan Plan) (ProductPage, error) {
	return s.store.ListProducts(ctx, ProductFilter{Status: StatusReview}, plan)
}

func (s *ReviewService) Approve(ctx context.Context, publicID string) (Product, error) {
	return s.store.ReviewProduct(ctx, publicID, StatusOn, "")
}

func (s *ReviewService) Reject(ctx context.Context, publicID, reason string) (Product, error) {
	if strings.TrimSpace(reason) == "" {
		return Product{}, ErrReviewReasonRequired
	}
	return s.store.ReviewProduct(ctx, publicID, StatusRejected, strings.TrimSpace(reason))
}
