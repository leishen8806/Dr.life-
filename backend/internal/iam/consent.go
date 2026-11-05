package iam

import (
	"context"
	"errors"
	"time"
)

type LegalDocumentKind string

const (
	LegalDocumentPrivacy LegalDocumentKind = "privacy"
	LegalDocumentTerms   LegalDocumentKind = "terms"
)

var (
	ErrInvalidLegalDocumentKind = errors.New("invalid legal document kind")
	ErrNoPublishedLegalDocument = errors.New("no published legal document")
	// ErrLegalDocumentNotFound is retained for callers that only need the public not-found semantics.
	ErrLegalDocumentNotFound    = ErrNoPublishedLegalDocument
	ErrConsentVersionNotCurrent = errors.New("consent version is not current")
)

// NoPublishedLegalDocumentError identifies the agreement type that has no published version.
// It intentionally contains no document body or account data.
type NoPublishedLegalDocumentError struct{ Kind LegalDocumentKind }

func (e NoPublishedLegalDocumentError) Error() string {
	return "no published legal document for " + string(e.Kind)
}

func (NoPublishedLegalDocumentError) Unwrap() error { return ErrNoPublishedLegalDocument }

func (e NoPublishedLegalDocumentError) NoPublishedLegalDocument() string { return string(e.Kind) }

func (k LegalDocumentKind) Valid() bool {
	return k == LegalDocumentPrivacy || k == LegalDocumentTerms
}

type LegalDocument struct {
	Kind        LegalDocumentKind
	Version     string
	EffectiveAt time.Time
	BodyZH      string
	BodyEN      string
	BodyKM      string
}

type ConsentStatus struct {
	Kind          LegalDocumentKind
	Version       string
	Accepted      bool
	NeedsReaccept bool
}

// ConsentRepository is IAM's narrow persistence boundary for legal documents.
type ConsentRepository interface {
	Current(context.Context, LegalDocumentKind, time.Time) (LegalDocument, error)
	AcceptCurrent(context.Context, string, LegalDocumentKind, string, string, time.Time) error
	Revoke(context.Context, string, LegalDocumentKind, time.Time) error
	HasActive(context.Context, string, LegalDocumentKind, string) (bool, error)
}

type ConsentService struct {
	store ConsentRepository
	now   func() time.Time
}

func NewConsentService(store ConsentRepository, now func() time.Time) *ConsentService {
	if now == nil {
		now = time.Now
	}
	return &ConsentService{store: store, now: now}
}

func (s *ConsentService) Accept(ctx context.Context, accountID string, kind LegalDocumentKind, version, channel string) error {
	if s == nil || s.store == nil || accountID == "" || !kind.Valid() || version == "" || channel == "" {
		return ErrInvalidLegalDocumentKind
	}
	return s.store.AcceptCurrent(ctx, accountID, kind, version, channel, s.now().UTC())
}

func (s *ConsentService) Revoke(ctx context.Context, accountID string, kind LegalDocumentKind) error {
	if s == nil || s.store == nil || accountID == "" || !kind.Valid() {
		return ErrInvalidLegalDocumentKind
	}
	return s.store.Revoke(ctx, accountID, kind, s.now().UTC())
}

func (s *ConsentService) Status(ctx context.Context, accountID string) ([]ConsentStatus, error) {
	if s == nil || s.store == nil {
		return nil, ErrInvalidLegalDocumentKind
	}
	statuses := make([]ConsentStatus, 0, 2)
	for _, kind := range []LegalDocumentKind{LegalDocumentPrivacy, LegalDocumentTerms} {
		current, err := s.store.Current(ctx, kind, s.now().UTC())
		if err != nil {
			if errors.Is(err, ErrNoPublishedLegalDocument) {
				statuses = append(statuses, ConsentStatus{Kind: kind})
				continue
			}
			return nil, err
		}
		accepted, err := s.store.HasActive(ctx, accountID, kind, current.Version)
		if err != nil {
			return nil, err
		}
		statuses = append(statuses, ConsentStatus{Kind: kind, Version: current.Version, Accepted: accepted, NeedsReaccept: !accepted})
	}
	return statuses, nil
}

// UnpublishedKinds returns only kinds that currently lack an effective published
// document. Other persistence failures remain errors and must never be treated
// as a fail-open condition.
func (s *ConsentService) UnpublishedKinds(ctx context.Context, _ string) ([]string, error) {
	if s == nil || s.store == nil {
		return nil, ErrInvalidLegalDocumentKind
	}
	missing := make([]string, 0, 2)
	for _, kind := range []LegalDocumentKind{LegalDocumentPrivacy, LegalDocumentTerms} {
		_, err := s.store.Current(ctx, kind, s.now().UTC())
		if errors.Is(err, ErrNoPublishedLegalDocument) {
			missing = append(missing, string(kind))
			continue
		}
		if err != nil {
			return nil, err
		}
	}
	return missing, nil
}

func (s *ConsentService) NeedsConsent(ctx context.Context, accountID string) (bool, error) {
	if s == nil || s.store == nil {
		return false, ErrInvalidLegalDocumentKind
	}
	statuses, err := s.Status(ctx, accountID)
	if err != nil {
		return false, err
	}
	for _, status := range statuses {
		if status.NeedsReaccept {
			return true, nil
		}
	}
	return false, nil
}

func (s *ConsentService) CurrentDocument(ctx context.Context, kind LegalDocumentKind) (LegalDocument, error) {
	if s == nil || s.store == nil || !kind.Valid() {
		return LegalDocument{}, ErrInvalidLegalDocumentKind
	}
	return s.store.Current(ctx, kind, s.now().UTC())
}
