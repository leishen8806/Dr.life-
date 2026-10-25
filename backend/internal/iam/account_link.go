// Package iam owns account identity, roles, and account-link business rules.
package iam

import (
	"context"
	"errors"
)

var (
	ErrAccountLinkForbidden = errors.New("account link forbidden")
	ErrAccountLinkNotFound  = errors.New("account link not found")
)

type AccountLink struct {
	PublicID          string
	BuyerAccountID    string
	MerchantAccountID string
	Unlinked          bool
}

// AccountLinkCandidate is deliberately limited to public account identifiers.
// It never exposes the Telegram number used for the server-side match.
type AccountLinkCandidate struct{ AccountID string }

type AccountLinkAudit struct{ ActorAccountID, Action, LinkPublicID string }

// AccountLinkRepository is the IAM-owned persistence boundary. Its production
// implementation must enforce the same-person and active-owner predicates in SQL.
type AccountLinkRepository interface {
	IsActiveMerchantOwner(context.Context, string) (bool, error)
	CandidateBuyers(context.Context, string) ([]AccountLinkCandidate, error)
	Link(context.Context, AccountLink) (AccountLink, error)
	Unlink(context.Context, string, string) error
	Audit(context.Context, AccountLinkAudit) error
}

type AccountLinkService struct {
	repo  AccountLinkRepository
	newID func() (string, error)
}

func NewAccountLinkService(repo AccountLinkRepository, newID func() (string, error)) *AccountLinkService {
	return &AccountLinkService{repo: repo, newID: newID}
}

func (s *AccountLinkService) Link(ctx context.Context, actorAccountID, buyerAccountID string) (AccountLink, error) {
	owner, err := s.repo.IsActiveMerchantOwner(ctx, actorAccountID)
	if err != nil {
		return AccountLink{}, err
	}
	if !owner {
		return AccountLink{}, ErrAccountLinkForbidden
	}
	candidates, err := s.repo.CandidateBuyers(ctx, actorAccountID)
	if err != nil {
		return AccountLink{}, err
	}
	found := false
	for _, candidate := range candidates {
		if candidate.AccountID == buyerAccountID {
			found = true
			break
		}
	}
	if !found {
		return AccountLink{}, ErrAccountLinkForbidden
	}
	publicID, err := s.newID()
	if err != nil {
		return AccountLink{}, err
	}
	link, err := s.repo.Link(ctx, AccountLink{PublicID: publicID, BuyerAccountID: buyerAccountID, MerchantAccountID: actorAccountID})
	if err != nil {
		return AccountLink{}, err
	}
	if err := s.repo.Audit(ctx, AccountLinkAudit{ActorAccountID: actorAccountID, Action: "account_linked", LinkPublicID: link.PublicID}); err != nil {
		return AccountLink{}, err
	}
	return link, nil
}

func (s *AccountLinkService) Unlink(ctx context.Context, actorAccountID, linkPublicID string) error {
	owner, err := s.repo.IsActiveMerchantOwner(ctx, actorAccountID)
	if err != nil {
		return err
	}
	if !owner {
		return ErrAccountLinkForbidden
	}
	if err := s.repo.Unlink(ctx, linkPublicID, actorAccountID); err != nil {
		return err
	}
	return s.repo.Audit(ctx, AccountLinkAudit{ActorAccountID: actorAccountID, Action: "account_unlinked", LinkPublicID: linkPublicID})
}
