package catalog_test

import (
	"context"
	"errors"
	"testing"

	"github.com/leishen8806/Dr.life-/backend/internal/catalog"
)

type reviewStore struct {
	product catalog.Product
	to      catalog.ProductStatus
	reason  string
}

func (s *reviewStore) ListProducts(context.Context, catalog.ProductFilter, catalog.Plan) (catalog.ProductPage, error) {
	return catalog.ProductPage{Items: []catalog.Product{s.product}, Total: 1}, nil
}

func (s *reviewStore) ReviewProduct(_ context.Context, publicID string, to catalog.ProductStatus, reason string) (catalog.Product, error) {
	if publicID != s.product.PublicID {
		return catalog.Product{}, catalog.ErrNotFound
	}
	if s.product.Status != catalog.StatusReview {
		return catalog.Product{}, catalog.ErrInvalidReviewTransition
	}
	s.to, s.reason = to, reason
	s.product.Status = to
	return s.product, nil
}

func TestReviewServiceApprovesOnlyReviewProducts(t *testing.T) {
	store := &reviewStore{product: catalog.Product{PublicID: productULID, Status: catalog.StatusReview}}
	service := catalog.NewReviewService(store)

	got, err := service.Approve(context.Background(), productULID)
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if got.Status != catalog.StatusOn || store.to != catalog.StatusOn {
		t.Fatalf("status=%q/%q, want on", got.Status, store.to)
	}
}

func TestReviewServiceRequiresReasonWhenRejecting(t *testing.T) {
	store := &reviewStore{product: catalog.Product{PublicID: productULID, Status: catalog.StatusReview}}
	service := catalog.NewReviewService(store)

	if _, err := service.Reject(context.Background(), productULID, " "); !errors.Is(err, catalog.ErrReviewReasonRequired) {
		t.Fatalf("Reject error=%v, want ErrReviewReasonRequired", err)
	}
	if store.to != "" {
		t.Fatalf("store mutated on invalid rejection: %#v", store)
	}
}

func TestValidateReviewDecisionRejectsNonReviewProducts(t *testing.T) {
	if err := catalog.ValidateReviewDecision(catalog.StatusDraft, catalog.StatusOn, ""); !errors.Is(err, catalog.ErrInvalidReviewTransition) {
		t.Fatalf("error=%v, want ErrInvalidReviewTransition", err)
	}
	if err := catalog.ValidateReviewDecision(catalog.StatusReview, catalog.StatusRejected, " "); !errors.Is(err, catalog.ErrReviewReasonRequired) {
		t.Fatalf("error=%v, want ErrReviewReasonRequired", err)
	}
	if err := catalog.ValidateReviewDecision(catalog.StatusReview, catalog.StatusRejected, "missing dimensions"); err != nil {
		t.Fatalf("valid rejection error=%v", err)
	}
}
