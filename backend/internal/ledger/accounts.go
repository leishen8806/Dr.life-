package ledger

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5"
)

type WalletAccounts struct {
	Available int64
	Frozen    int64
	AdBudget  int64
}

type singletonCache struct {
	mu  sync.Mutex
	ids map[string]int64
}

var singletonKinds = []string{"BANK_DEDICATED", "PLATFORM_HELD", "PAYABLE_CARRIER", "SUSPENSE", "PENDING_PAYOUT", "DELIVERY_ESCROW"}

func (s *Service) EnsureWallet(ctx context.Context, ownerAccountID int64, merchantID *int64) (WalletAccounts, error) {
	if ownerAccountID <= 0 || merchantID != nil && *merchantID <= 0 {
		return WalletAccounts{}, ErrInvalidRequest
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return WalletAccounts{}, err
	}
	defer tx.Rollback(ctx)
	kinds := []string{"WALLET_AVAIL", "WALLET_FROZEN"}
	if merchantID != nil {
		kinds = append(kinds, "AD_BUDGET")
	}
	for _, kind := range kinds {
		if _, err := tx.Exec(ctx, `
			INSERT INTO ledger.account(kind,normal_side,owner_account,merchant_id,allow_negative)
			VALUES($1,'CREDIT',$2,$3,false)
			ON CONFLICT ON CONSTRAINT uq_account_kind_owner DO NOTHING`, kind, ownerAccountID, merchantID); err != nil {
			return WalletAccounts{}, err
		}
	}
	rows, err := tx.Query(ctx, `SELECT kind,id,merchant_id FROM ledger.account WHERE owner_account=$1 AND kind=ANY($2)`, ownerAccountID, kinds)
	if err != nil {
		return WalletAccounts{}, err
	}
	defer rows.Close()
	var result WalletAccounts
	for rows.Next() {
		var kind string
		var id int64
		var storedMerchant *int64
		if err := rows.Scan(&kind, &id, &storedMerchant); err != nil {
			return WalletAccounts{}, err
		}
		if !sameInt64(storedMerchant, merchantID) {
			return WalletAccounts{}, ErrIdempotencyConflict
		}
		switch kind {
		case "WALLET_AVAIL":
			result.Available = id
		case "WALLET_FROZEN":
			result.Frozen = id
		case "AD_BUDGET":
			result.AdBudget = id
		}
	}
	if err := rows.Err(); err != nil {
		return WalletAccounts{}, err
	}
	if result.Available == 0 || result.Frozen == 0 || merchantID != nil && result.AdBudget == 0 {
		return WalletAccounts{}, ErrAccountNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return WalletAccounts{}, err
	}
	return result, nil
}

func (s *Service) singletonAccounts(ctx context.Context) (map[string]int64, error) {
	s.singletons.mu.Lock()
	defer s.singletons.mu.Unlock()
	if s.singletons.ids != nil {
		return s.singletons.ids, nil
	}
	ids := make(map[string]int64, len(singletonKinds))
	for _, kind := range singletonKinds {
		var id int64
		if err := s.db.QueryRow(ctx, `SELECT id FROM ledger.account WHERE kind=$1 AND owner_account IS NULL`, kind).Scan(&id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, fmt.Errorf("%w: %s", ErrAccountNotFound, kind)
			}
			return nil, err
		}
		ids[kind] = id
	}
	s.singletons.ids = ids
	return ids, nil
}

func requireKinds(expected map[int64]string, extra transactionCheck) transactionCheck {
	return func(ctx context.Context, tx pgx.Tx) error {
		for id, kind := range expected {
			var actual string
			if err := tx.QueryRow(ctx, `SELECT kind FROM ledger.account WHERE id=$1`, id).Scan(&actual); err != nil {
				return err
			}
			if actual != kind {
				return fmt.Errorf("%w: account %d must be %s", ErrInvalidRequest, id, kind)
			}
		}
		if extra != nil {
			return extra(ctx, tx)
		}
		return nil
	}
}
