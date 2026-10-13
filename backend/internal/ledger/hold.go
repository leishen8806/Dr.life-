package ledger

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrHoldNotFound = errors.New("ledger hold not found")
	ErrHoldState    = errors.New("ledger hold state does not allow this operation")
	ErrHoldDisputed = errors.New("ledger hold is disputed")
)

type Hold struct {
	ID            int64
	AccountID     int64
	RefType       string
	RefID         int64
	Amount        int64
	Status        string
	FreezeTxnID   int64
	CloseTxnID    *int64
	CreatedAt     time.Time
	ClosedAt      *time.Time
	DisputeReason *string
	DisputedBy    *int64
	DisputedAt    *time.Time
	ResolvedBy    *int64
}

type FreezeHoldInput struct {
	RefType                       string
	RefID                         int64
	WalletAvailable, WalletFrozen int64
	Amount, ActorID               int64
}

type FreezeHoldResult struct {
	Hold Hold
	Post PostResult
}

type SettleHoldInput struct {
	HoldID, PayeeAvailable int64
	Fee, ActorID           int64
}

type RefundHoldInput struct {
	HoldID, ActorID, PayerActorID int64
}

type DisputeHoldInput struct {
	HoldID, ActorID int64
	Reason          string
}

type ResolveDisputeInput struct {
	HoldID, ResolvedBy, PayerActorID int64
	Resolution                       string
	PayeeAvailable                   int64
}

type HoldMismatch struct {
	AccountID   int64
	Balance     int64
	OpenHoldSum int64
}

func (s *Service) FreezeHold(ctx context.Context, in FreezeHoldInput) (FreezeHoldResult, error) {
	if !keySegmentPattern.MatchString(in.RefType) || in.RefID <= 0 || in.Amount <= 0 || in.ActorID <= 0 {
		return FreezeHoldResult{}, ErrInvalidRequest
	}
	req := businessRequest("freeze", in.Amount, fmt.Sprintf("freeze:hold:%s-%d", in.RefType, in.RefID), in.RefType, &in.RefID, in.ActorID,
		Posting{AccountID: in.WalletAvailable, Side: "DEBIT", Amount: in.Amount},
		Posting{AccountID: in.WalletFrozen, Side: "CREDIT", Amount: in.Amount})

	var holdID int64
	hooks := transactionHooks{
		check: requireKinds(map[int64]string{in.WalletAvailable: "WALLET_AVAIL", in.WalletFrozen: "WALLET_FROZEN"}, func(ctx context.Context, tx pgx.Tx) error {
			var existingID int64
			err := tx.QueryRow(ctx, `SELECT id FROM ledger.hold WHERE ref_type=$1 AND ref_id=$2 FOR UPDATE`, in.RefType, in.RefID).Scan(&existingID)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			result, err := loadExisting(ctx, tx, req)
			if err == nil {
				return &transactionReplay{result: result}
			}
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrIdempotencyConflict
			}
			return err
		},
			[]int64{in.WalletAvailable, in.WalletFrozen}),
		after: func(ctx context.Context, tx pgx.Tx, result PostResult) error {
			err := tx.QueryRow(ctx, `
				INSERT INTO ledger.hold(account_id,ref_type,ref_id,amount,status,freeze_txn_id)
				VALUES($1,$2,$3,$4,'held',$5)
				ON CONFLICT (ref_type,ref_id) DO NOTHING RETURNING id`,
				in.WalletFrozen, in.RefType, in.RefID, in.Amount, result.TxnID).Scan(&holdID)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrIdempotencyConflict
			}
			return err
		},
		replayCheck: func(ctx context.Context, tx pgx.Tx, result PostResult) error {
			hold, err := queryHold(ctx, tx.QueryRow(ctx, holdSelect+` WHERE ref_type=$1 AND ref_id=$2 FOR UPDATE`, in.RefType, in.RefID))
			if err != nil || hold.Status != "held" || hold.AccountID != in.WalletFrozen || hold.Amount != in.Amount || hold.FreezeTxnID != result.TxnID {
				return ErrIdempotencyConflict
			}
			holdID = hold.ID
			return nil
		},
	}
	post, err := s.postWithHooks(ctx, req, hooks)
	if err != nil {
		return FreezeHoldResult{}, err
	}
	hold, err := s.GetHold(ctx, holdID)
	if err != nil {
		return FreezeHoldResult{}, err
	}
	return FreezeHoldResult{Hold: hold, Post: post}, nil
}

func (s *Service) SettleHold(ctx context.Context, in SettleHoldInput) (PostResult, error) {
	if in.HoldID <= 0 || in.PayeeAvailable <= 0 || in.ActorID <= 0 || in.Fee != 0 {
		return PostResult{}, ErrInvalidRequest
	}
	return s.closeHold(ctx, in.HoldID, "released", in.PayeeAvailable, in.ActorID, 0, 0, false)
}

func (s *Service) RefundHold(ctx context.Context, in RefundHoldInput) (PostResult, error) {
	if in.HoldID <= 0 || in.ActorID <= 0 || in.PayerActorID <= 0 {
		return PostResult{}, ErrInvalidRequest
	}
	if in.ActorID == in.PayerActorID {
		return PostResult{}, ErrSeparationDuty
	}
	return s.closeHold(ctx, in.HoldID, "refunded", 0, in.ActorID, in.PayerActorID, 0, false)
}

func (s *Service) DisputeHold(ctx context.Context, in DisputeHoldInput) (Hold, error) {
	reason := strings.TrimSpace(in.Reason)
	if in.HoldID <= 0 || in.ActorID <= 0 || reason == "" {
		return Hold{}, ErrInvalidRequest
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return Hold{}, err
	}
	defer tx.Rollback(ctx)
	hold, err := queryHold(ctx, tx.QueryRow(ctx, holdSelect+` WHERE id=$1 FOR UPDATE`, in.HoldID))
	if err != nil {
		return Hold{}, mapHoldNotFound(err)
	}
	if hold.Status != "held" {
		return Hold{}, holdStateError(hold.Status)
	}
	if _, err := tx.Exec(ctx, `UPDATE ledger.hold SET status='disputed',dispute_reason=$2,disputed_by=$3,disputed_at=now() WHERE id=$1`, in.HoldID, reason, in.ActorID); err != nil {
		return Hold{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Hold{}, err
	}
	return s.GetHold(ctx, in.HoldID)
}

func (s *Service) ResolveDispute(ctx context.Context, in ResolveDisputeInput) (PostResult, error) {
	if in.HoldID <= 0 || in.ResolvedBy <= 0 || in.PayerActorID <= 0 || in.ResolvedBy == in.PayerActorID {
		if in.ResolvedBy > 0 && in.ResolvedBy == in.PayerActorID {
			return PostResult{}, ErrSeparationDuty
		}
		return PostResult{}, ErrInvalidRequest
	}
	switch in.Resolution {
	case "release":
		if in.PayeeAvailable <= 0 {
			return PostResult{}, ErrInvalidRequest
		}
		return s.closeHold(ctx, in.HoldID, "released", in.PayeeAvailable, in.ResolvedBy, in.PayerActorID, in.ResolvedBy, true)
	case "refund":
		return s.closeHold(ctx, in.HoldID, "refunded", 0, in.ResolvedBy, in.PayerActorID, in.ResolvedBy, true)
	default:
		return PostResult{}, ErrInvalidRequest
	}
}

// closeHold fixes the global lock order used by hold accounting: Post first locks
// balance rows in ascending account_id order, then the transaction check locks the hold row.
func (s *Service) closeHold(ctx context.Context, holdID int64, target string, payee, actor, payer, resolvedBy int64, disputed bool) (PostResult, error) {
	snapshot, err := s.GetHold(ctx, holdID)
	if err != nil {
		return PostResult{}, err
	}
	if disputed {
		if snapshot.Status != "disputed" && snapshot.Status != target {
			return PostResult{}, holdStateError(snapshot.Status)
		}
	} else if snapshot.Status != "held" && snapshot.Status != target {
		return PostResult{}, holdStateError(snapshot.Status)
	}

	walletAvailable := int64(0)
	if target == "refunded" {
		walletAvailable, err = s.availableWalletForFrozen(ctx, snapshot.AccountID)
		if err != nil {
			return PostResult{}, err
		}
	}
	postings := []Posting{{AccountID: snapshot.AccountID, Side: "DEBIT", Amount: snapshot.Amount}}
	expected := map[int64]string{snapshot.AccountID: "WALLET_FROZEN"}
	if target == "released" {
		postings = append(postings, Posting{AccountID: payee, Side: "CREDIT", Amount: snapshot.Amount})
		expected[payee] = "WALLET_AVAIL"
	} else {
		postings = append(postings, Posting{AccountID: walletAvailable, Side: "CREDIT", Amount: snapshot.Amount})
		expected[walletAvailable] = "WALLET_AVAIL"
	}
	typ := "settle"
	if target == "refunded" {
		typ = "refund"
	}
	req := businessRequest(typ, snapshot.Amount, fmt.Sprintf("%s:hold:%d", typ, holdID), "hold", &holdID, actor, postings...)
	if payer > 0 {
		req.Note = fmt.Sprintf("payer_actor:%d", payer)
	}
	check := requireKinds(expected, func(ctx context.Context, tx pgx.Tx) error {
		locked, err := queryHold(ctx, tx.QueryRow(ctx, holdSelect+` WHERE id=$1 FOR UPDATE`, holdID))
		if err != nil {
			return mapHoldNotFound(err)
		}
		if !sameHoldSnapshot(locked, snapshot) {
			result, replayErr := loadExisting(ctx, tx, req)
			if replayErr == nil {
				return &transactionReplay{result: result}
			}
			if !errors.Is(replayErr, pgx.ErrNoRows) {
				return replayErr
			}
			return holdStateError(locked.Status)
		}
		return nil
	})
	hooks := transactionHooks{
		check: check,
		after: func(ctx context.Context, tx pgx.Tx, result PostResult) error {
			command, err := tx.Exec(ctx, `UPDATE ledger.hold SET status=$2,close_txn_id=$3,closed_at=now(),resolved_by=NULLIF($4,0) WHERE id=$1`, holdID, target, result.TxnID, resolvedBy)
			if err != nil {
				return err
			}
			if command.RowsAffected() != 1 {
				return ErrHoldState
			}
			return nil
		},
		replayCheck: func(ctx context.Context, tx pgx.Tx, result PostResult) error {
			hold, err := queryHold(ctx, tx.QueryRow(ctx, holdSelect+` WHERE id=$1 FOR UPDATE`, holdID))
			if err != nil || hold.Status != target || hold.CloseTxnID == nil || *hold.CloseTxnID != result.TxnID || resolvedBy != valueOrZero(hold.ResolvedBy) {
				return ErrIdempotencyConflict
			}
			return nil
		},
	}
	return s.postWithHooks(ctx, req, hooks)
}

func (s *Service) GetHold(ctx context.Context, id int64) (Hold, error) {
	if id <= 0 {
		return Hold{}, ErrInvalidRequest
	}
	hold, err := queryHold(ctx, s.db.QueryRow(ctx, holdSelect+` WHERE id=$1`, id))
	return hold, mapHoldNotFound(err)
}

func (s *Service) ListHoldsByAccount(ctx context.Context, accountID int64, statuses []string) ([]Hold, error) {
	if accountID <= 0 {
		return nil, ErrInvalidRequest
	}
	for _, status := range statuses {
		if status != "held" && status != "disputed" && status != "released" && status != "refunded" {
			return nil, ErrInvalidRequest
		}
	}
	rows, err := s.db.Query(ctx, holdSelect+` WHERE account_id=$1 AND (COALESCE(cardinality($2::text[]),0)=0 OR status=ANY($2)) ORDER BY id`, accountID, statuses)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Hold, 0)
	for rows.Next() {
		hold, err := scanHold(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, hold)
	}
	return result, rows.Err()
}

func (s *Service) VerifyHoldsMatchFrozen(ctx context.Context) ([]HoldMismatch, error) {
	rows, err := s.db.Query(ctx, `
		SELECT a.id,b.balance,COALESCE(sum(h.amount) FILTER (WHERE h.status IN ('held','disputed')),0)
		FROM ledger.account a JOIN ledger.balance b ON b.account_id=a.id
		LEFT JOIN ledger.hold h ON h.account_id=a.id
		WHERE a.kind='WALLET_FROZEN'
		GROUP BY a.id,b.balance
		HAVING b.balance <> COALESCE(sum(h.amount) FILTER (WHERE h.status IN ('held','disputed')),0)
		ORDER BY a.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var mismatches []HoldMismatch
	for rows.Next() {
		var mismatch HoldMismatch
		if err := rows.Scan(&mismatch.AccountID, &mismatch.Balance, &mismatch.OpenHoldSum); err != nil {
			return nil, err
		}
		mismatches = append(mismatches, mismatch)
	}
	return mismatches, rows.Err()
}

const holdSelect = `SELECT id,account_id,ref_type,ref_id,amount,status,freeze_txn_id,close_txn_id,created_at,closed_at,dispute_reason,disputed_by,disputed_at,resolved_by FROM ledger.hold`

type rowScanner interface{ Scan(...any) error }

func queryHold(_ context.Context, row rowScanner) (Hold, error) { return scanHold(row) }

func scanHold(row rowScanner) (Hold, error) {
	var hold Hold
	err := row.Scan(&hold.ID, &hold.AccountID, &hold.RefType, &hold.RefID, &hold.Amount, &hold.Status,
		&hold.FreezeTxnID, &hold.CloseTxnID, &hold.CreatedAt, &hold.ClosedAt, &hold.DisputeReason,
		&hold.DisputedBy, &hold.DisputedAt, &hold.ResolvedBy)
	return hold, err
}

func mapHoldNotFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrHoldNotFound
	}
	return err
}

func holdStateError(status string) error {
	if status == "disputed" {
		return ErrHoldDisputed
	}
	return ErrHoldState
}

func sameHoldSnapshot(left, right Hold) bool {
	return left.ID == right.ID && left.AccountID == right.AccountID && left.Amount == right.Amount && left.Status == right.Status && left.FreezeTxnID == right.FreezeTxnID
}

func valueOrZero(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func (s *Service) availableWalletForFrozen(ctx context.Context, frozenID int64) (int64, error) {
	var available int64
	err := s.db.QueryRow(ctx, `
		SELECT avail.id FROM ledger.account frozen
		JOIN ledger.account avail ON avail.owner_account=frozen.owner_account AND avail.kind='WALLET_AVAIL'
		WHERE frozen.id=$1 AND frozen.kind='WALLET_FROZEN'`, frozenID).Scan(&available)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrAccountOwnerMismatch
	}
	return available, err
}
