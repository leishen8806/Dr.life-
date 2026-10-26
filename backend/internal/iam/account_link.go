// Package iam owns account identity, roles, and account-link business rules.
package iam

import (
	"context"
	"errors"
)

var (
	ErrAccountLinkForbidden = errors.New("account link forbidden")
	ErrAccountLinkNotFound  = errors.New("account link not found")
	ErrAccountLinkExists    = errors.New("account link already exists")
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

// AccountLinkAudit is written to iam.merchant_op_log in the same transaction as the
// link change. LogPublicID is the log row's own identifier and must be unique per
// entry; it must not reuse the link's identifier, which is shared by link and unlink.
type AccountLinkAudit struct{ ActorAccountID, Action, LinkPublicID, LogPublicID string }

// AccountLinkRepository is the IAM-owned persistence boundary. Its production
// implementation must enforce the same-person and active-owner predicates in SQL.
type AccountLinkRepository interface {
	IsActiveMerchantOwner(context.Context, string) (bool, error)
	CandidateBuyers(context.Context, string) ([]AccountLinkCandidate, error)
	// Link and Unlink must apply the change and its audit entry atomically.
	Link(context.Context, AccountLink, AccountLinkAudit) (AccountLink, error)
	Unlink(context.Context, string, string, AccountLinkAudit) error
}

type AccountLinkService struct {
	repo  AccountLinkRepository
	newID func() (string, error)
}

func NewAccountLinkService(repo AccountLinkRepository, newID func() (string, error)) *AccountLinkService {
	return &AccountLinkService{repo: repo, newID: newID}
}

// CandidateBuyers returns only the IAM-filtered buyer account identifiers for a merchant account.
// It intentionally delegates matching to the repository so no other module reads iam tables directly.
func (s *AccountLinkService) CandidateBuyers(ctx context.Context, merchantAccountID string) ([]AccountLinkCandidate, error) {
	return s.repo.CandidateBuyers(ctx, merchantAccountID)
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
	logID, err := s.newID()
	if err != nil {
		return AccountLink{}, err
	}
	return s.repo.Link(ctx,
		AccountLink{PublicID: publicID, BuyerAccountID: buyerAccountID, MerchantAccountID: actorAccountID},
		AccountLinkAudit{ActorAccountID: actorAccountID, Action: "account_linked", LinkPublicID: publicID, LogPublicID: logID})
}

func (s *AccountLinkService) Unlink(ctx context.Context, actorAccountID, linkPublicID string) error {
	owner, err := s.repo.IsActiveMerchantOwner(ctx, actorAccountID)
	if err != nil {
		return err
	}
	if !owner {
		return ErrAccountLinkForbidden
	}
	logID, err := s.newID()
	if err != nil {
		return err
	}
	return s.repo.Unlink(ctx, linkPublicID, actorAccountID,
		AccountLinkAudit{ActorAccountID: actorAccountID, Action: "account_unlinked", LinkPublicID: linkPublicID, LogPublicID: logID})
}
