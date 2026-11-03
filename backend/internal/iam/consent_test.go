package iam

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestConsentServiceRequiresCurrentVersionAndSupportsRevocation(t *testing.T) {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	store := &fakeConsentStore{current: map[LegalDocumentKind]LegalDocument{
		LegalDocumentPrivacy: {Kind: LegalDocumentPrivacy, Version: "1.1", EffectiveAt: now},
		LegalDocumentTerms:   {Kind: LegalDocumentTerms, Version: "1.0", EffectiveAt: now},
	}}
	service := NewConsentService(store, func() time.Time { return now })
	ctx := context.Background()

	if err := service.Accept(ctx, "account-1", LegalDocumentPrivacy, "1.0", "mini_app"); !errors.Is(err, ErrConsentVersionNotCurrent) {
		t.Fatalf("accepting a superseded version error = %v, want ErrConsentVersionNotCurrent", err)
	}
	if err := service.Accept(ctx, "account-1", LegalDocumentPrivacy, "1.1", "mini_app"); err != nil {
		t.Fatalf("accept current privacy: %v", err)
	}
	if err := service.Accept(ctx, "account-1", LegalDocumentTerms, "1.0", "mini_app"); err != nil {
		t.Fatalf("accept current terms: %v", err)
	}

	needs, err := service.NeedsConsent(ctx, "account-1")
	if err != nil || needs {
		t.Fatalf("needs after accepting current versions = %v, %v; want false, nil", needs, err)
	}
	if err := service.Revoke(ctx, "account-1", LegalDocumentPrivacy); err != nil {
		t.Fatalf("revoke privacy: %v", err)
	}
	needs, err = service.NeedsConsent(ctx, "account-1")
	if err != nil || !needs {
		t.Fatalf("needs after revocation = %v, %v; want true, nil", needs, err)
	}
}

func TestConsentServicePreservesNoPublishedDocumentSentinel(t *testing.T) {
	store := &fakeConsentStore{missingErr: NoPublishedLegalDocumentError{Kind: LegalDocumentTerms}}
	service := NewConsentService(store, time.Now)

	_, err := service.CurrentDocument(context.Background(), LegalDocumentTerms)
	if !errors.Is(err, ErrNoPublishedLegalDocument) {
		t.Fatalf("error=%v, want ErrNoPublishedLegalDocument", err)
	}
	var noPublished NoPublishedLegalDocumentError
	if !errors.As(err, &noPublished) || noPublished.Kind != LegalDocumentTerms {
		t.Fatalf("error=%v, want terms NoPublishedLegalDocumentError", err)
	}
}

type fakeConsentStore struct {
	current    map[LegalDocumentKind]LegalDocument
	accepted   map[LegalDocumentKind]string
	missingErr error
}

func (s *fakeConsentStore) Current(_ context.Context, kind LegalDocumentKind, _ time.Time) (LegalDocument, error) {
	if s.missingErr != nil {
		return LegalDocument{}, s.missingErr
	}
	doc, ok := s.current[kind]
	if !ok {
		return LegalDocument{}, ErrLegalDocumentNotFound
	}
	return doc, nil
}

func (s *fakeConsentStore) AcceptCurrent(_ context.Context, _ string, kind LegalDocumentKind, version, _ string, _ time.Time) error {
	if s.current[kind].Version != version {
		return ErrConsentVersionNotCurrent
	}
	if s.accepted == nil {
		s.accepted = map[LegalDocumentKind]string{}
	}
	s.accepted[kind] = version
	return nil
}

func (s *fakeConsentStore) Revoke(_ context.Context, _ string, kind LegalDocumentKind, _ time.Time) error {
	delete(s.accepted, kind)
	return nil
}

func (s *fakeConsentStore) HasActive(_ context.Context, _ string, kind LegalDocumentKind, version string) (bool, error) {
	return s.accepted[kind] == version, nil
}
