package iam

import (
	"context"
	"errors"
	"testing"
)

func TestAccountLinkServiceOwnerOnlyAndImmutableBusinessData(t *testing.T) {
	repo := &fakeAccountLinkRepository{owners: map[string]bool{"owner": true}, candidates: map[string][]AccountLinkCandidate{"owner": {{AccountID: "buyer"}}}}
	counter := 0
	service := NewAccountLinkService(repo, func() (string, error) {
		counter++
		return "01ARZ3NDEKTSV4RRFFQ69G5F" + string(rune('A'+counter)), nil
	})
	if _, err := service.Link(context.Background(), "staff", "buyer"); !errors.Is(err, ErrAccountLinkForbidden) {
		t.Fatalf("staff Link error=%v, want forbidden", err)
	}
	link, err := service.Link(context.Background(), "owner", "buyer")
	if err != nil {
		t.Fatal(err)
	}
	if link.PublicID == "" || len(repo.audits) != 1 || repo.audits[0].Action != "account_linked" {
		t.Fatalf("link/audit=%+v/%+v", link, repo.audits)
	}
	if err := service.Unlink(context.Background(), "owner", link.PublicID); err != nil {
		t.Fatal(err)
	}
	if len(repo.audits) != 2 || repo.audits[1].Action != "account_unlinked" {
		t.Fatalf("unlink audit=%+v", repo.audits)
	}
	// The two audit rows must not share an identifier: the unlink entry used to reuse the link's id.
	if repo.audits[0].LogPublicID == "" || repo.audits[0].LogPublicID == repo.audits[1].LogPublicID {
		t.Fatalf("audit log ids must be distinct and non-empty: %+v", repo.audits)
	}
}

type fakeAccountLinkRepository struct {
	owners     map[string]bool
	candidates map[string][]AccountLinkCandidate
	links      map[string]AccountLink
	audits     []AccountLinkAudit
}

func (r *fakeAccountLinkRepository) IsActiveMerchantOwner(_ context.Context, accountID string) (bool, error) {
	return r.owners[accountID], nil
}
func (r *fakeAccountLinkRepository) CandidateBuyers(_ context.Context, merchantAccountID string) ([]AccountLinkCandidate, error) {
	return r.candidates[merchantAccountID], nil
}
func (r *fakeAccountLinkRepository) Link(_ context.Context, link AccountLink, audit AccountLinkAudit) (AccountLink, error) {
	if r.links == nil {
		r.links = map[string]AccountLink{}
	}
	r.links[link.PublicID] = link
	r.audits = append(r.audits, audit)
	return link, nil
}
func (r *fakeAccountLinkRepository) Unlink(_ context.Context, id, actor string, audit AccountLinkAudit) error {
	l, ok := r.links[id]
	if !ok {
		return ErrAccountLinkNotFound
	}
	if actor != "owner" {
		return ErrAccountLinkForbidden
	}
	l.Unlinked = true
	r.links[id] = l
	r.audits = append(r.audits, audit)
	return nil
}
