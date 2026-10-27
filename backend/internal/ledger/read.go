package ledger

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// queryPool is deliberately read-only in production.  The type exposes no
// mutation methods so the L-09a read surface cannot change balances or journal
// rows even accidentally.
type queryPool interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// ReadService is the ledger report-reader.  Construct it only with the report
// pool configured with default_transaction_read_only=on.
type ReadService struct{ db queryPool }

func NewReadService(db queryPool) *ReadService { return &ReadService{db: db} }

type WalletTransactionQuery struct {
	OwnerAccountID int64
	PageSize       int
	Cursor         string
}

type WalletTransaction struct {
	TxnNo      string
	OccurredAt time.Time
	Type       string
	// AmountE4 is a signed integer in 0.0001 USD units from the wallet owner's
	// perspective. It is never a floating-point display amount.
	AmountE4 int64
	RefType  string
	RefID    *int64
	Kind     string
}

type WalletTransactionPage struct {
	Items      []WalletTransaction
	NextCursor string
}

type walletCursor struct {
	OccurredAt time.Time `json:"occurred_at"`
	TxnID      int64     `json:"txn_id"`
}

type AdminTransaction struct {
	TxnNo      string
	OccurredAt time.Time
	Type       string
	AmountE4   int64
	RefType    string
	RefID      *int64
	Postings   []AdminPosting
}

type AdminAccountReconciliation struct {
	AccountRef          string
	Kind                string
	SystemBalanceE4     int64
	RecomputedBalanceE4 int64
	DiffE4              int64
	LastCheckedAt       *time.Time
}

type AdminPosting struct {
	TxnNo      string
	OccurredAt time.Time
	Type       string
	PostingSeq int64
	Side       string
	AmountE4   int64
	Role       string
	Category   string
}

// ListWalletTransactions returns only transactions whose postings belong to
// the caller's wallet accounts.  It intentionally never joins iam: ownership
// is the ledger.account.owner_account value resolved by the IAM adapter.
func (s *ReadService) ListWalletTransactions(ctx context.Context, query WalletTransactionQuery) (WalletTransactionPage, error) {
	if s == nil || s.db == nil || query.OwnerAccountID <= 0 {
		return WalletTransactionPage{}, ErrInvalidRequest
	}
	limit := query.PageSize
	if limit <= 0 {
		limit = 30
	}
	if limit > 100 {
		return WalletTransactionPage{}, ErrInvalidRequest
	}
	var cursor *walletCursor
	if query.Cursor != "" {
		decoded, err := decodeWalletCursor(query.Cursor)
		if err != nil {
			return WalletTransactionPage{}, ErrInvalidRequest
		}
		cursor = &decoded
	}
	rows, err := s.db.Query(ctx, `
		SELECT t.id,t.txn_no,t.occurred_at,t.type,
		       SUM(CASE WHEN p.side=a.normal_side THEN p.amount ELSE -p.amount END)::bigint,
		       COALESCE(t.ref_type,''),t.ref_id
		FROM ledger.txn t
		JOIN ledger.posting p ON p.txn_id=t.id
		JOIN ledger.account a ON a.id=p.account_id
		WHERE a.owner_account=$1
		  AND a.kind IN ('WALLET_AVAIL','WALLET_FROZEN','AD_BUDGET')
		  AND ($2::timestamptz IS NULL OR (t.occurred_at,t.id) < ($2,$3))
		GROUP BY t.id,t.txn_no,t.occurred_at,t.type,t.ref_type,t.ref_id
		ORDER BY t.occurred_at DESC,t.id DESC
		LIMIT $4`, query.OwnerAccountID, cursorTime(cursor), cursorID(cursor), limit+1)
	if err != nil {
		return WalletTransactionPage{}, fmt.Errorf("list wallet transactions: %w", err)
	}
	defer rows.Close()
	page := WalletTransactionPage{Items: make([]WalletTransaction, 0, limit)}
	var last walletCursor
	for rows.Next() {
		var item WalletTransaction
		var txnID int64
		if err := rows.Scan(&txnID, &item.TxnNo, &item.OccurredAt, &item.Type, &item.AmountE4, &item.RefType, &item.RefID); err != nil {
			return WalletTransactionPage{}, fmt.Errorf("scan wallet transaction: %w", err)
		}
		if len(page.Items) == limit {
			page.NextCursor = encodeWalletCursor(last)
			break
		}
		item.Kind = "txn"
		page.Items = append(page.Items, item)
		last = walletCursor{OccurredAt: item.OccurredAt, TxnID: txnID}
	}
	if err := rows.Err(); err != nil {
		return WalletTransactionPage{}, fmt.Errorf("iterate wallet transactions: %w", err)
	}
	return page, nil
}

func encodeWalletCursor(cursor walletCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeWalletCursor(value string) (walletCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return walletCursor{}, err
	}
	var cursor walletCursor
	if err := json.Unmarshal(raw, &cursor); err != nil || cursor.OccurredAt.IsZero() || cursor.TxnID <= 0 {
		return walletCursor{}, fmt.Errorf("invalid wallet cursor")
	}
	return cursor, nil
}

func cursorTime(cursor *walletCursor) *time.Time {
	if cursor == nil {
		return nil
	}
	return &cursor.OccurredAt
}

func cursorID(cursor *walletCursor) int64 {
	if cursor == nil {
		return 0
	}
	return cursor.TxnID
}

func (s *ReadService) ListAdminTransactions(ctx context.Context, pageSize int) ([]AdminTransaction, error) {
	if s == nil || s.db == nil {
		return nil, ErrInvalidRequest
	}
	if pageSize <= 0 {
		pageSize = 30
	}
	if pageSize > 100 {
		return nil, ErrInvalidRequest
	}
	rows, err := s.db.Query(ctx, `SELECT txn_no,occurred_at,type,amount,COALESCE(ref_type,''),ref_id FROM ledger.txn ORDER BY occurred_at DESC,id DESC LIMIT $1`, pageSize)
	if err != nil {
		return nil, fmt.Errorf("list admin transactions: %w", err)
	}
	defer rows.Close()
	items := make([]AdminTransaction, 0, pageSize)
	for rows.Next() {
		var item AdminTransaction
		if err := rows.Scan(&item.TxnNo, &item.OccurredAt, &item.Type, &item.AmountE4, &item.RefType, &item.RefID); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *ReadService) GetAdminTransaction(ctx context.Context, txnNo string) (AdminTransaction, bool, error) {
	if s == nil || s.db == nil || txnNo == "" {
		return AdminTransaction{}, false, ErrInvalidRequest
	}
	rows, err := s.db.Query(ctx, `SELECT id,txn_no,occurred_at,type,amount,COALESCE(ref_type,''),ref_id FROM ledger.txn WHERE txn_no=$1`, txnNo)
	if err != nil {
		return AdminTransaction{}, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return AdminTransaction{}, false, rows.Err()
	}
	var item AdminTransaction
	var txnID int64
	if err := rows.Scan(&txnID, &item.TxnNo, &item.OccurredAt, &item.Type, &item.AmountE4, &item.RefType, &item.RefID); err != nil {
		return AdminTransaction{}, false, err
	}
	postingRows, err := s.db.Query(ctx, `SELECT t.txn_no,t.occurred_at,t.type,p.id,p.side,p.amount,COALESCE(p.role,''),COALESCE(p.category,'') FROM ledger.posting p JOIN ledger.txn t ON t.id=p.txn_id WHERE p.txn_id=$1 ORDER BY p.id`, txnID)
	if err != nil {
		return AdminTransaction{}, false, err
	}
	defer postingRows.Close()
	for postingRows.Next() {
		var posting AdminPosting
		if err := postingRows.Scan(&posting.TxnNo, &posting.OccurredAt, &posting.Type, &posting.PostingSeq, &posting.Side, &posting.AmountE4, &posting.Role, &posting.Category); err != nil {
			return AdminTransaction{}, false, err
		}
		item.Postings = append(item.Postings, posting)
	}
	if err := postingRows.Err(); err != nil {
		return AdminTransaction{}, false, err
	}
	return item, true, nil
}

func (s *ReadService) ListAccountPostings(ctx context.Context, accountID int64, pageSize int) ([]AdminPosting, error) {
	if s == nil || s.db == nil || accountID <= 0 {
		return nil, ErrInvalidRequest
	}
	if pageSize <= 0 {
		pageSize = 30
	}
	if pageSize > 100 {
		return nil, ErrInvalidRequest
	}
	rows, err := s.db.Query(ctx, `SELECT t.txn_no,t.occurred_at,t.type,p.id,p.side,p.amount,COALESCE(p.role,''),COALESCE(p.category,'') FROM ledger.posting p JOIN ledger.txn t ON t.id=p.txn_id WHERE p.account_id=$1 ORDER BY t.occurred_at DESC,p.id DESC LIMIT $2`, accountID, pageSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]AdminPosting, 0, pageSize)
	for rows.Next() {
		var item AdminPosting
		if err := rows.Scan(&item.TxnNo, &item.OccurredAt, &item.Type, &item.PostingSeq, &item.Side, &item.AmountE4, &item.Role, &item.Category); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *ReadService) GetWalletTransaction(ctx context.Context, ownerAccountID int64, txnNo string) (WalletTransaction, bool, error) {
	if s == nil || s.db == nil || ownerAccountID <= 0 || txnNo == "" {
		return WalletTransaction{}, false, ErrInvalidRequest
	}
	rows, err := s.db.Query(ctx, `SELECT t.txn_no,t.occurred_at,t.type,SUM(CASE WHEN p.side=a.normal_side THEN p.amount ELSE -p.amount END)::bigint,COALESCE(t.ref_type,''),t.ref_id FROM ledger.txn t JOIN ledger.posting p ON p.txn_id=t.id JOIN ledger.account a ON a.id=p.account_id WHERE a.owner_account=$1 AND a.kind IN ('WALLET_AVAIL','WALLET_FROZEN','AD_BUDGET') AND t.txn_no=$2 GROUP BY t.id,t.txn_no,t.occurred_at,t.type,t.ref_type,t.ref_id`, ownerAccountID, txnNo)
	if err != nil {
		return WalletTransaction{}, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return WalletTransaction{}, false, err
		}
		return WalletTransaction{}, false, nil
	}
	var item WalletTransaction
	if err := rows.Scan(&item.TxnNo, &item.OccurredAt, &item.Type, &item.AmountE4, &item.RefType, &item.RefID); err != nil {
		return WalletTransaction{}, false, err
	}
	item.Kind = "txn"
	return item, true, nil
}

func (s *ReadService) ListAccountReconciliations(ctx context.Context) ([]AdminAccountReconciliation, error) {
	if s == nil || s.db == nil {
		return nil, ErrInvalidRequest
	}
	rows, err := s.db.Query(ctx, `SELECT a.id::text,a.kind,b.balance,COALESCE(s.balance,b.balance),b.balance-COALESCE(s.balance,b.balance),r.finished_at FROM ledger.account a JOIN ledger.balance b ON b.account_id=a.id LEFT JOIN LATERAL (SELECT day,balance FROM ledger.snapshot_daily WHERE account_id=a.id ORDER BY day DESC LIMIT 1) s ON true LEFT JOIN LATERAL (SELECT finished_at FROM ledger.recon_run WHERE day=s.day AND kind='daily' AND status='ok' ORDER BY attempt DESC LIMIT 1) r ON true ORDER BY a.id`)
	if err != nil {
		return nil, fmt.Errorf("list account reconciliations: %w", err)
	}
	defer rows.Close()
	items := make([]AdminAccountReconciliation, 0)
	for rows.Next() {
		var item AdminAccountReconciliation
		if err := rows.Scan(&item.AccountRef, &item.Kind, &item.SystemBalanceE4, &item.RecomputedBalanceE4, &item.DiffE4, &item.LastCheckedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}
