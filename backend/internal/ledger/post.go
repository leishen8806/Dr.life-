package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrInvalidRequest      = errors.New("invalid ledger request")
	ErrInvalidAmount       = errors.New("invalid ledger amount")
	ErrUnbalanced          = errors.New("ledger transaction is unbalanced")
	ErrAccountNotFound     = errors.New("ledger account not found")
	ErrAccountClosed       = errors.New("ledger account is closed")
	ErrInsufficientFunds   = errors.New("ERR_INSUFFICIENT_FUNDS")
	ErrIdempotencyConflict = errors.New("ledger idempotency key conflicts with an existing transaction")
)

var idemKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+:[A-Za-z0-9_-]+:[A-Za-z0-9_-]+(?::[A-Za-z0-9_-]+)?$`)
var adjustIdemKeyPattern = regexp.MustCompile(`^adjust:[A-Za-z0-9_-]+-[1-9][0-9]*$`)

var validTxnTypes = map[string]struct{}{
	"topup": {}, "freeze": {}, "settle": {}, "refund": {}, "withdraw_req": {},
	"payout": {}, "payout_fail": {}, "storage_fee": {}, "delivery_hold": {},
	"delivery_release": {}, "delivery_refund": {}, "ad_charge": {}, "ad_spend": {},
	"ad_refund": {}, "member_fee": {}, "sweep": {}, "carrier_pay": {}, "reversal": {},
	"adjust": {}, "bank_diff": {},
}

var validCategories = map[string]struct{}{
	"storage": {}, "delivery_margin": {}, "ads": {}, "settle_fee": {}, "member": {},
}

type beginner interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type Service struct {
	db                       beginner
	maxRetries               int
	singletons               singletonCache
	accountGuard             AccountGuard
	payeeCipher              PayeeCipher
	withdrawalMetrics        WithdrawalMetrics
	reconciliationMetrics    ReconciliationMetrics
	largeWithdrawalThreshold int64
	reconciliationLocation   *time.Location
	spotCheckEvery           uint64
	spotCheckCounter         atomic.Uint64
	now                      func() time.Time
}

type Options struct {
	MaxRetries               int
	AccountGuard             AccountGuard
	PayeeCipher              PayeeCipher
	WithdrawalMetrics        WithdrawalMetrics
	ReconciliationMetrics    ReconciliationMetrics
	LargeWithdrawalThreshold int64
	ReconciliationLocation   *time.Location
	SpotCheckEvery           int
	Now                      func() time.Time
}

func New(db beginner, options Options) *Service {
	maxRetries := options.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	threshold := options.LargeWithdrawalThreshold
	if threshold <= 0 {
		threshold = 10_000_000
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	location := options.ReconciliationLocation
	if location == nil {
		location = time.FixedZone("Asia/Phnom_Penh", 7*60*60)
	}
	spotCheckEvery := options.SpotCheckEvery
	if spotCheckEvery < 0 {
		spotCheckEvery = 0
	}
	return &Service{
		db: db, maxRetries: maxRetries, accountGuard: options.AccountGuard,
		payeeCipher: options.PayeeCipher, withdrawalMetrics: options.WithdrawalMetrics,
		reconciliationMetrics: options.ReconciliationMetrics, reconciliationLocation: location,
		largeWithdrawalThreshold: threshold, spotCheckEvery: uint64(spotCheckEvery), now: now,
	}
}

type Posting struct {
	AccountID int64
	Side      string
	Amount    int64
	Role      string
	Category  string
}

type PostRequest struct {
	Type          string
	Amount        int64
	IdemKey       string
	RefType       string
	RefID         *int64
	Note          string
	ActorID       int64
	OccurredAt    *time.Time
	ReversesTxnID *int64
	Postings      []Posting
}

type Balance struct {
	AccountID int64 `json:"account_id"`
	Balance   int64 `json:"balance"`
}

type PostResult struct {
	TxnID           int64     `json:"txn_id"`
	TxnNo           string    `json:"txn_no"`
	Replayed        bool      `json:"-"`
	ReversedByTxnID *int64    `json:"reversed_by_txn_id,omitempty"`
	Balances        []Balance `json:"balances"`
}

type transactionCheck func(context.Context, pgx.Tx) error
type transactionAfter func(context.Context, pgx.Tx, PostResult) error
type transactionReplayCheck func(context.Context, pgx.Tx, PostResult) error

type transactionReplay struct{ result PostResult }

func (*transactionReplay) Error() string { return "ledger transaction replay" }

type transactionHooks struct {
	check       transactionCheck
	after       transactionAfter
	replayCheck transactionReplayCheck
	// skipInitialReplay lets a business-owned unique key resolve its record
	// under the transaction check before comparing the ledger transaction.
	skipInitialReplay bool
}

func (s *Service) postRaw(ctx context.Context, request PostRequest, check transactionCheck) (PostResult, error) {
	return s.postWithHooks(ctx, request, transactionHooks{check: check})
}

func (s *Service) postWithHooks(ctx context.Context, request PostRequest, hooks transactionHooks) (PostResult, error) {
	if err := validateRequest(request); err != nil {
		return PostResult{}, err
	}
	var result PostResult
	var err error
	for attempt := 0; attempt <= s.maxRetries; attempt++ {
		result, err = s.postOnce(ctx, request, hooks)
		if err == nil {
			if !result.Replayed {
				s.maybeSpotCheck(request.Postings)
			}
			return result, nil
		}
		if !isRetryable(err) || attempt == s.maxRetries {
			return result, err
		}
		delay := time.Duration(1<<attempt) * 10 * time.Millisecond
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return PostResult{}, ctx.Err()
		case <-timer.C:
		}
	}
	return result, err
}

type accountState struct {
	id            int64
	kind          string
	normalSide    string
	status        string
	allowNegative bool
	balance       int64
}

func (s *Service) postOnce(ctx context.Context, request PostRequest, hooks transactionHooks) (PostResult, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return PostResult{}, err
	}
	defer tx.Rollback(ctx)
	if !hooks.skipInitialReplay {
		if result, replayErr := loadExisting(ctx, tx, request); replayErr == nil {
			if hooks.replayCheck != nil {
				if err := hooks.replayCheck(ctx, tx, result); err != nil {
					return PostResult{}, err
				}
			}
			if err := tx.Commit(ctx); err != nil {
				return PostResult{}, err
			}
			return result, nil
		} else if !errors.Is(replayErr, pgx.ErrNoRows) {
			return PostResult{}, replayErr
		}
	}

	ids := sortedAccountIDs(request.Postings)
	accounts := make(map[int64]accountState, len(ids))
	for _, id := range ids {
		var account accountState
		err = tx.QueryRow(ctx, `
			SELECT a.id,a.kind,a.normal_side,a.status,a.allow_negative,b.balance
			FROM ledger.account a JOIN ledger.balance b ON b.account_id=a.id
			WHERE a.id=$1 FOR UPDATE OF b`, id).
			Scan(&account.id, &account.kind, &account.normalSide, &account.status, &account.allowNegative, &account.balance)
		if errors.Is(err, pgx.ErrNoRows) {
			return PostResult{}, fmt.Errorf("%w: %d", ErrAccountNotFound, id)
		}
		if err != nil {
			return PostResult{}, err
		}
		if account.status != "open" {
			return PostResult{}, fmt.Errorf("%w: %d", ErrAccountClosed, id)
		}
		accounts[id] = account
	}
	if hooks.check != nil {
		if err := hooks.check(ctx, tx); err != nil {
			var replay *transactionReplay
			if errors.As(err, &replay) {
				if hooks.replayCheck != nil {
					if err := hooks.replayCheck(ctx, tx, replay.result); err != nil {
						return PostResult{}, err
					}
				}
				if err := tx.Commit(ctx); err != nil {
					return PostResult{}, err
				}
				return replay.result, nil
			}
			return PostResult{}, err
		}
	}

	var txnID int64
	var storedTxnNo string
	err = tx.QueryRow(ctx, `
		INSERT INTO ledger.txn(txn_no,type,amount,idem_key,ref_type,ref_id,note,reverses_txn_id,actor_id,occurred_at)
		VALUES('L'||lpad(nextval('ledger.txn_no_seq')::text,12,'0'),$1,$2,$3,NULLIF($4,''),$5,NULLIF($6,''),$7,$8,COALESCE($9,now()))
		ON CONFLICT (idem_key) DO NOTHING
		RETURNING id,txn_no`, request.Type, request.Amount, request.IdemKey,
		request.RefType, request.RefID, request.Note, request.ReversesTxnID, request.ActorID, request.OccurredAt).
		Scan(&txnID, &storedTxnNo)
	if errors.Is(err, pgx.ErrNoRows) {
		result, replayErr := loadExisting(ctx, tx, request)
		if replayErr != nil {
			return PostResult{}, replayErr
		}
		if hooks.replayCheck != nil {
			if err := hooks.replayCheck(ctx, tx, result); err != nil {
				return PostResult{}, err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return PostResult{}, err
		}
		return result, nil
	}
	if err != nil {
		return PostResult{}, err
	}

	for _, posting := range request.Postings {
		account := accounts[posting.AccountID]
		if posting.Category != "" && account.kind != "PLATFORM_HELD" {
			return PostResult{}, fmt.Errorf("%w: category requires PLATFORM_HELD", ErrInvalidRequest)
		}
		delta := posting.Amount
		if posting.Side != account.normalSide {
			delta = -delta
		}
		balance, ok := checkedAdd(account.balance, delta)
		if !ok {
			return PostResult{}, ErrInvalidAmount
		}
		if !account.allowNegative && balance < 0 {
			return PostResult{}, ErrInsufficientFunds
		}
		account.balance = balance
		accounts[posting.AccountID] = account
	}

	for _, posting := range request.Postings {
		if _, err := tx.Exec(ctx, `
			INSERT INTO ledger.posting(txn_id,account_id,side,amount,role,category)
			VALUES($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''))`, txnID, posting.AccountID, posting.Side,
			posting.Amount, posting.Role, posting.Category); err != nil {
			return PostResult{}, err
		}
	}

	result := PostResult{TxnID: txnID, TxnNo: storedTxnNo, Balances: make([]Balance, 0, len(ids))}
	for _, id := range ids {
		account := accounts[id]
		if _, err := tx.Exec(ctx, `UPDATE ledger.balance SET balance=$2,version=version+1,updated_at=now() WHERE account_id=$1`, id, account.balance); err != nil {
			return PostResult{}, err
		}
		result.Balances = append(result.Balances, Balance{AccountID: id, Balance: account.balance})
	}
	if hooks.after != nil {
		if err := hooks.after(ctx, tx, result); err != nil {
			return PostResult{}, err
		}
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return PostResult{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ledger.outbox(txn_id,topic,payload) VALUES($1,'ledger.txn.posted',$2)`, txnID, payload); err != nil {
		return PostResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PostResult{}, err
	}
	return result, nil
}

func validateRequest(request PostRequest) error {
	if _, ok := validTxnTypes[request.Type]; !ok || request.ActorID <= 0 || len(request.Postings) < 2 {
		return ErrInvalidRequest
	}
	if request.Type == "reversal" && request.ReversesTxnID == nil || request.Type != "reversal" && request.ReversesTxnID != nil {
		return ErrInvalidRequest
	}
	if request.Amount <= 0 {
		return ErrInvalidAmount
	}
	if len(request.IdemKey) > 200 || !idemKeyPattern.MatchString(request.IdemKey) &&
		(request.Type != "adjust" || !adjustIdemKeyPattern.MatchString(request.IdemKey)) {
		return ErrInvalidRequest
	}
	seen := make(map[int64]struct{}, len(request.Postings))
	var debit, credit int64
	for _, posting := range request.Postings {
		if posting.AccountID <= 0 || posting.Amount <= 0 || posting.Side != "DEBIT" && posting.Side != "CREDIT" {
			return ErrInvalidRequest
		}
		if posting.Category != "" {
			if _, ok := validCategories[posting.Category]; !ok {
				return ErrInvalidRequest
			}
		}
		if _, exists := seen[posting.AccountID]; exists {
			return ErrInvalidRequest
		}
		seen[posting.AccountID] = struct{}{}
		var ok bool
		if posting.Side == "DEBIT" {
			debit, ok = checkedAdd(debit, posting.Amount)
		} else {
			credit, ok = checkedAdd(credit, posting.Amount)
		}
		if !ok {
			return ErrInvalidAmount
		}
	}
	if debit != credit || debit != request.Amount {
		return ErrUnbalanced
	}
	return nil
}

func sortedAccountIDs(postings []Posting) []int64 {
	ids := make([]int64, 0, len(postings))
	seen := make(map[int64]struct{}, len(postings))
	for _, posting := range postings {
		if _, ok := seen[posting.AccountID]; !ok {
			seen[posting.AccountID] = struct{}{}
			ids = append(ids, posting.AccountID)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func checkedAdd(left, right int64) (int64, bool) {
	if right > 0 && left > math.MaxInt64-right || right < 0 && left < math.MinInt64-right {
		return 0, false
	}
	return left + right, true
}

func isRetryable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == "40001" || pgErr.Code == "40P01")
}
