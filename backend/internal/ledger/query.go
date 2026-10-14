package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (s *Service) GetByID(ctx context.Context, txnID int64) (PostResult, error) {
	result, err := queryResult(ctx, s.db, `
		SELECT t.id,t.txn_no,o.payload
		FROM ledger.txn t
		LEFT JOIN LATERAL (
			SELECT payload FROM ledger.outbox
			WHERE txn_id=t.id AND topic='ledger.txn.posted' ORDER BY id LIMIT 1
		) o ON true
		WHERE t.id=$1`, txnID)
	if err != nil {
		return PostResult{}, err
	}
	var reversalID int64
	if err := s.db.QueryRow(ctx, `SELECT id FROM ledger.txn WHERE reverses_txn_id=$1`, txnID).Scan(&reversalID); err == nil {
		result.ReversedByTxnID = &reversalID
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return PostResult{}, err
	}
	return result, nil
}

func (s *Service) GetByIdemKey(ctx context.Context, idemKey string) (PostResult, error) {
	return queryResult(ctx, s.db, `
		SELECT t.id,t.txn_no,o.payload
		FROM ledger.txn t
		LEFT JOIN LATERAL (
			SELECT payload FROM ledger.outbox
			WHERE txn_id=t.id AND topic='ledger.txn.posted' ORDER BY id LIMIT 1
		) o ON true
		WHERE t.idem_key=$1`, idemKey)
}

func queryResult(ctx context.Context, db rowQuerier, statement string, value any) (PostResult, error) {
	var result PostResult
	var payload []byte
	if err := db.QueryRow(ctx, statement, value).Scan(&result.TxnID, &result.TxnNo, &payload); err != nil {
		return PostResult{}, err
	}
	if payload == nil {
		result.Replayed = true
		return result, nil
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		return PostResult{}, err
	}
	return result, nil
}

type storedPosting struct {
	accountID int64
	side      string
	amount    int64
	role      string
	category  string
}

func loadExisting(ctx context.Context, tx pgx.Tx, request PostRequest) (PostResult, error) {
	var result PostResult
	var txnType, refType, note string
	var amount, actorID int64
	var refID, reversesTxnID *int64
	var occurredAt time.Time
	err := tx.QueryRow(ctx, `
		SELECT id,txn_no,type,amount,COALESCE(ref_type,''),ref_id,COALESCE(note,''),reverses_txn_id,actor_id,occurred_at
		FROM ledger.txn WHERE idem_key=$1`, request.IdemKey).
		Scan(&result.TxnID, &result.TxnNo, &txnType, &amount, &refType, &refID, &note, &reversesTxnID, &actorID, &occurredAt)
	if err != nil {
		return PostResult{}, err
	}
	if txnType != request.Type || amount != request.Amount || refType != request.RefType || !sameInt64(refID, request.RefID) ||
		note != request.Note || !sameInt64(reversesTxnID, request.ReversesTxnID) || actorID != request.ActorID ||
		request.OccurredAt != nil && !occurredAt.Equal(request.OccurredAt.Truncate(time.Microsecond)) {
		return PostResult{}, ErrIdempotencyConflict
	}

	rows, err := tx.Query(ctx, `SELECT account_id,side,amount,COALESCE(role,''),COALESCE(category,'') FROM ledger.posting WHERE txn_id=$1`, result.TxnID)
	if err != nil {
		return PostResult{}, err
	}
	defer rows.Close()
	stored := make([]storedPosting, 0, len(request.Postings))
	for rows.Next() {
		var posting storedPosting
		if err := rows.Scan(&posting.accountID, &posting.side, &posting.amount, &posting.role, &posting.category); err != nil {
			return PostResult{}, err
		}
		stored = append(stored, posting)
	}
	if err := rows.Err(); err != nil {
		return PostResult{}, err
	}
	if !samePostings(stored, request.Postings) {
		return PostResult{}, ErrIdempotencyConflict
	}
	var payload []byte
	if err := tx.QueryRow(ctx, `SELECT payload FROM ledger.outbox WHERE txn_id=$1 AND topic='ledger.txn.posted' ORDER BY id LIMIT 1`, result.TxnID).Scan(&payload); errors.Is(err, pgx.ErrNoRows) {
		result.Replayed = true
		return result, nil
	} else if err != nil {
		return PostResult{}, err
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		return PostResult{}, err
	}
	result.Replayed = true
	return result, nil
}

func sameInt64(left, right *int64) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func samePostings(stored []storedPosting, requested []Posting) bool {
	if len(stored) != len(requested) {
		return false
	}
	want := make([]storedPosting, len(requested))
	for i, posting := range requested {
		want[i] = storedPosting{posting.AccountID, posting.Side, posting.Amount, posting.Role, posting.Category}
	}
	sortStored := func(items []storedPosting) {
		sort.Slice(items, func(i, j int) bool {
			if items[i].accountID != items[j].accountID {
				return items[i].accountID < items[j].accountID
			}
			return items[i].side < items[j].side
		})
	}
	sortStored(stored)
	sortStored(want)
	for i := range stored {
		if stored[i] != want[i] {
			return false
		}
	}
	return true
}
