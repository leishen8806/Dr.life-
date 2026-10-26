package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	CheckBalanceCache  = "BALANCE_CACHE"
	CheckTxnBalanced   = "TXN_BALANCED"
	CheckIdentity      = "IDENTITY"
	CheckHoldFrozen    = "HOLD_FROZEN"
	CheckPendingPayout = "PENDING_PAYOUT"
	CheckReversal      = "REVERSAL"
	CheckOrphan        = "ORPHAN"
	CheckSnapshotDrift = "SNAPSHOT_DRIFT"
)

var (
	ErrPayoutFrozen              = errors.New("ledger payouts are frozen")
	ErrPayoutFreezeNotFound      = errors.New("ledger payout freeze not found")
	ErrPayoutFreezeReleased      = errors.New("ledger payout freeze already released")
	ErrFreezeReleaseSeparation   = errors.New("payout freeze releaser must differ from reconciliation initiator")
	ErrReconciliationForceReason = errors.New("forced reconciliation requires a reason")
	ErrReconciliationDayOpen     = errors.New("reconciliation day has not ended")
	ErrReconciliationRunning     = errors.New("ledger reconciliation is already running")
)

type ReconciliationMetrics interface {
	ReconRun(ok bool)
	ReconMismatch(checkCode string, count int)
	ReconSpotCheckDropped()
}

type ReconIssue struct {
	ID        int64
	RunID     int64
	CheckCode string
	AccountID *int64
	TxnID     *int64
	Expected  *int64
	Actual    *int64
	Detail    string
	CreatedAt time.Time
}

type ReconciliationRun struct {
	ID          int64
	Day         time.Time
	Kind        string
	Attempt     int
	StartedBy   *int64
	ForceReason string
	StartedAt   time.Time
	FinishedAt  *time.Time
	Status      string
	Summary     map[string]int
	Issues      []ReconIssue
}

type ReconciliationOptions struct {
	Force       bool
	Reason      string
	InitiatedBy int64
}

type SnapshotResult struct {
	Day    time.Time
	Drifts []ReconIssue
}

type PayoutFreeze struct {
	ID            int64
	Scope         string
	AccountID     *int64
	Reason        string
	RunID         int64
	CreatedAt     time.Time
	ReleasedAt    *time.Time
	ReleasedBy    *int64
	ReleaseReason *string
}

type reconciliationEvent struct {
	RunID      int64          `json:"run_id"`
	Day        string         `json:"day"`
	Counts     map[string]int `json:"counts"`
	AccountIDs []int64        `json:"account_ids,omitempty"`
	OccurredAt time.Time      `json:"occurred_at"`
}

type freezeReleasedEvent struct {
	FreezeID   int64     `json:"freeze_id"`
	Scope      string    `json:"scope"`
	AccountID  *int64    `json:"account_id,omitempty"`
	ReleasedBy int64     `json:"released_by"`
	Reason     string    `json:"reason"`
	OccurredAt time.Time `json:"occurred_at"`
}

func (s *Service) SnapshotDaily(ctx context.Context, day time.Time) (SnapshotResult, error) {
	if !s.reconciliationDayClosed(day) {
		return SnapshotResult{}, ErrReconciliationDayOpen
	}
	day, cutoff := s.reconciliationDay(day)
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return SnapshotResult{}, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `
		WITH computed AS MATERIALIZED (
			SELECT a.id AS account_id,
				CASE a.normal_side
				WHEN 'DEBIT' THEN COALESCE(sum(CASE WHEN t.id IS NULL THEN 0 WHEN p.side='DEBIT' THEN p.amount ELSE -p.amount END),0)
				ELSE COALESCE(sum(CASE WHEN t.id IS NULL THEN 0 WHEN p.side='CREDIT' THEN p.amount ELSE -p.amount END),0)
				END::bigint AS balance
			FROM ledger.account a
			LEFT JOIN ledger.posting p ON p.account_id=a.id
			LEFT JOIN ledger.txn t ON t.id=p.txn_id AND t.created_at < $2
			GROUP BY a.id,a.normal_side
		), inserted AS (
			INSERT INTO ledger.snapshot_daily(day,account_id,balance)
			SELECT $1,account_id,balance FROM computed
			ON CONFLICT (day,account_id) DO NOTHING
		)
		SELECT c.account_id,c.balance,s.balance
		FROM computed c JOIN ledger.snapshot_daily s ON s.day=$1 AND s.account_id=c.account_id
		WHERE c.balance<>s.balance ORDER BY c.account_id`, day, cutoff)
	if err != nil {
		return SnapshotResult{}, err
	}
	defer rows.Close()
	result := SnapshotResult{Day: day}
	for rows.Next() {
		var accountID, expected, actual int64
		if err := rows.Scan(&accountID, &expected, &actual); err != nil {
			return SnapshotResult{}, err
		}
		result.Drifts = append(result.Drifts, issue(CheckSnapshotDrift, &accountID, nil, expected, actual, "stored daily snapshot differs from posting history"))
	}
	if err := rows.Err(); err != nil {
		return SnapshotResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SnapshotResult{}, err
	}
	return result, nil
}

func (s *Service) RunDailyReconciliation(ctx context.Context, day time.Time, opts ReconciliationOptions) (ReconciliationRun, error) {
	if opts.Force && strings.TrimSpace(opts.Reason) == "" {
		return ReconciliationRun{}, ErrReconciliationForceReason
	}
	if !s.reconciliationDayClosed(day) {
		return ReconciliationRun{}, ErrReconciliationDayOpen
	}
	day, _ = s.reconciliationDay(day)
	run, existing, err := s.startReconciliationRun(ctx, day, "daily", opts)
	if err != nil || existing {
		return run, err
	}
	snapshot, err := s.SnapshotDaily(ctx, day)
	if err != nil {
		s.failReconciliationRun(ctx, run.ID, err)
		return ReconciliationRun{}, err
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		s.failReconciliationRun(ctx, run.ID, err)
		return ReconciliationRun{}, err
	}
	issues, err := runReconciliationChecks(ctx, tx, nil)
	if err == nil {
		err = tx.Commit(ctx)
	} else {
		_ = tx.Rollback(ctx)
	}
	if err != nil {
		s.failReconciliationRun(ctx, run.ID, err)
		return ReconciliationRun{}, err
	}
	issues = append(snapshot.Drifts, issues...)
	finished, err := s.finishReconciliationRun(ctx, run, issues)
	if err != nil {
		s.failReconciliationRun(ctx, run.ID, err)
	}
	return finished, err
}

func (s *Service) GetReconciliationRun(ctx context.Context, id int64) (ReconciliationRun, error) {
	if id <= 0 {
		return ReconciliationRun{}, ErrInvalidRequest
	}
	return s.loadReconciliationRun(ctx, s.db, id)
}

func (s *Service) ListPayoutFreezes(ctx context.Context, activeOnly bool) ([]PayoutFreeze, error) {
	query := `SELECT id,scope,account_id,reason,run_id,created_at,released_at,released_by,release_reason FROM ledger.payout_freeze`
	if activeOnly {
		query += ` WHERE released_at IS NULL`
	}
	query += ` ORDER BY id`
	rows, err := s.db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []PayoutFreeze
	for rows.Next() {
		var item PayoutFreeze
		if err := rows.Scan(&item.ID, &item.Scope, &item.AccountID, &item.Reason, &item.RunID, &item.CreatedAt, &item.ReleasedAt, &item.ReleasedBy, &item.ReleaseReason); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Service) ReleasePayoutFreeze(ctx context.Context, freezeID, releasedBy int64, reason string) (PayoutFreeze, error) {
	reason = strings.TrimSpace(reason)
	if freezeID <= 0 || releasedBy <= 0 || reason == "" {
		return PayoutFreeze{}, ErrInvalidRequest
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return PayoutFreeze{}, err
	}
	defer tx.Rollback(ctx)
	var item PayoutFreeze
	var startedBy *int64
	err = tx.QueryRow(ctx, `
		SELECT f.id,f.scope,f.account_id,f.reason,f.run_id,f.created_at,f.released_at,f.released_by,f.release_reason,r.started_by
		FROM ledger.payout_freeze f JOIN ledger.recon_run r ON r.id=f.run_id
		WHERE f.id=$1 FOR UPDATE OF f`, freezeID).
		Scan(&item.ID, &item.Scope, &item.AccountID, &item.Reason, &item.RunID, &item.CreatedAt, &item.ReleasedAt, &item.ReleasedBy, &item.ReleaseReason, &startedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return PayoutFreeze{}, ErrPayoutFreezeNotFound
	}
	if err != nil {
		return PayoutFreeze{}, err
	}
	if item.ReleasedAt != nil {
		return PayoutFreeze{}, ErrPayoutFreezeReleased
	}
	if startedBy != nil && *startedBy > 0 && *startedBy == releasedBy {
		return PayoutFreeze{}, ErrFreezeReleaseSeparation
	}
	now := s.now()
	if _, err := tx.Exec(ctx, `UPDATE ledger.payout_freeze SET released_at=$2,released_by=$3,release_reason=$4 WHERE id=$1`, freezeID, now, releasedBy, reason); err != nil {
		return PayoutFreeze{}, err
	}
	payload, err := json.Marshal(freezeReleasedEvent{FreezeID: freezeID, Scope: item.Scope, AccountID: item.AccountID, ReleasedBy: releasedBy, Reason: reason, OccurredAt: now})
	if err != nil {
		return PayoutFreeze{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ledger.outbox(topic,payload) VALUES('ledger.payout_freeze.released',$1)`, payload); err != nil {
		return PayoutFreeze{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PayoutFreeze{}, err
	}
	item.ReleasedAt, item.ReleasedBy, item.ReleaseReason = &now, &releasedBy, &reason
	return item, nil
}

func (s *Service) startReconciliationRun(ctx context.Context, day time.Time, kind string, opts ReconciliationOptions) (ReconciliationRun, bool, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return ReconciliationRun{}, false, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("recon:%s:%s", kind, day.Format("2006-01-02"))); err != nil {
		return ReconciliationRun{}, false, err
	}
	var existingID int64
	err = tx.QueryRow(ctx, `SELECT id FROM ledger.recon_run WHERE day=$1 AND kind=$2 ORDER BY attempt DESC LIMIT 1 FOR UPDATE`, day, kind).Scan(&existingID)
	if err == nil {
		run, loadErr := s.loadReconciliationRun(ctx, tx, existingID)
		if loadErr != nil {
			return ReconciliationRun{}, false, loadErr
		}
		if run.Status == "running" {
			if s.now().Sub(run.StartedAt) < s.reconciliationRunTimeout {
				if !opts.Force {
					return run, true, ErrReconciliationRunning
				}
			} else {
				summary, marshalErr := json.Marshal(map[string]int{"STALE_RUN": 1})
				if marshalErr != nil {
					return ReconciliationRun{}, false, marshalErr
				}
				finished := s.now()
				if _, updateErr := tx.Exec(ctx, `UPDATE ledger.recon_run SET status='failed',finished_at=$2,summary=$3 WHERE id=$1`, run.ID, finished, summary); updateErr != nil {
					return ReconciliationRun{}, false, updateErr
				}
				payload, marshalErr := json.Marshal(map[string]any{"run_id": run.ID, "day": day.Format("2006-01-02"), "occurred_at": finished})
				if marshalErr != nil {
					return ReconciliationRun{}, false, marshalErr
				}
				if _, insertErr := tx.Exec(ctx, `INSERT INTO ledger.outbox(topic,payload) VALUES('ledger.recon.stale_run',$1)`, payload); insertErr != nil {
					return ReconciliationRun{}, false, insertErr
				}
				opts.Force, opts.Reason = true, "stale reconciliation run replaced"
			}
		} else if !opts.Force {
			if err := tx.Commit(ctx); err != nil {
				return ReconciliationRun{}, false, err
			}
			return run, true, nil
		}
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ReconciliationRun{}, false, err
	}
	var attempt int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(attempt),0)+1 FROM ledger.recon_run WHERE day=$1 AND kind=$2`, day, kind).Scan(&attempt); err != nil {
		return ReconciliationRun{}, false, err
	}
	var startedBy any
	if opts.InitiatedBy > 0 {
		startedBy = opts.InitiatedBy
	}
	run := ReconciliationRun{Day: day, Kind: kind, Attempt: attempt, Status: "running", ForceReason: strings.TrimSpace(opts.Reason), StartedAt: s.now()}
	if opts.InitiatedBy > 0 {
		run.StartedBy = &opts.InitiatedBy
	}
	err = tx.QueryRow(ctx, `INSERT INTO ledger.recon_run(day,kind,attempt,started_by,force_reason,started_at) VALUES($1,$2,$3,$4,NULLIF($5,''),$6) RETURNING id`, day, kind, attempt, startedBy, run.ForceReason, run.StartedAt).Scan(&run.ID)
	if err != nil {
		return ReconciliationRun{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ReconciliationRun{}, false, err
	}
	return run, false, nil
}

func (s *Service) finishReconciliationRun(ctx context.Context, run ReconciliationRun, issues []ReconIssue) (ReconciliationRun, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return ReconciliationRun{}, err
	}
	defer tx.Rollback(ctx)
	counts := make(map[string]int)
	accountSet := make(map[int64]struct{})
	for i := range issues {
		issues[i].RunID = run.ID
		counts[issues[i].CheckCode]++
		if issues[i].AccountID != nil {
			accountSet[*issues[i].AccountID] = struct{}{}
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO ledger.recon_issue(run_id,check_code,account_id,txn_id,expected,actual,detail)
			VALUES($1,$2,$3,$4,$5,$6,NULLIF($7,'')) RETURNING id,created_at`, run.ID, issues[i].CheckCode,
			issues[i].AccountID, issues[i].TxnID, issues[i].Expected, issues[i].Actual, issues[i].Detail).
			Scan(&issues[i].ID, &issues[i].CreatedAt); err != nil {
			return ReconciliationRun{}, err
		}
	}
	globalFreezeReason := ""
	for accountID := range accountSet {
		freezeAccountID := accountID
		if countsForAccount(issues, CheckHoldFrozen, accountID) > 0 {
			if err := tx.QueryRow(ctx, `SELECT a.id FROM ledger.account f JOIN ledger.account a ON a.owner_account=f.owner_account WHERE f.id=$1 AND f.kind='WALLET_FROZEN' AND a.kind='WALLET_AVAIL'`, accountID).Scan(&freezeAccountID); errors.Is(err, pgx.ErrNoRows) {
				globalFreezeReason = CheckHoldFrozen + " wallet mapping missing"
				continue
			} else if err != nil {
				return ReconciliationRun{}, err
			}
		}
		if countsForAccount(issues, CheckBalanceCache, accountID) > 0 || countsForAccount(issues, CheckHoldFrozen, accountID) > 0 {
			if _, err := tx.Exec(ctx, `INSERT INTO ledger.payout_freeze(scope,account_id,reason,run_id) VALUES('account',$1,$2,$3) ON CONFLICT DO NOTHING`, freezeAccountID, "account reconciliation mismatch", run.ID); err != nil {
				return ReconciliationRun{}, err
			}
		}
	}
	if globalFreezeReason == "" {
		for _, code := range []string{CheckPendingPayout, CheckTxnBalanced, CheckIdentity, CheckSnapshotDrift, CheckReversal, CheckOrphan} {
			if counts[code] > 0 {
				globalFreezeReason = code + " mismatch"
				break
			}
		}
	}
	if globalFreezeReason != "" {
		if _, err := tx.Exec(ctx, `INSERT INTO ledger.payout_freeze(scope,reason,run_id) VALUES('global',$1,$2) ON CONFLICT DO NOTHING`, globalFreezeReason, run.ID); err != nil {
			return ReconciliationRun{}, err
		}
	}
	status := "ok"
	if len(issues) > 0 {
		status = "failed"
		accountIDs := make([]int64, 0, len(accountSet))
		for id := range accountSet {
			accountIDs = append(accountIDs, id)
		}
		sort.Slice(accountIDs, func(i, j int) bool { return accountIDs[i] < accountIDs[j] })
		if len(accountIDs) > 20 {
			accountIDs = accountIDs[:20]
		}
		payload, err := json.Marshal(reconciliationEvent{RunID: run.ID, Day: run.Day.Format("2006-01-02"), Counts: counts, AccountIDs: accountIDs, OccurredAt: s.now()})
		if err != nil {
			return ReconciliationRun{}, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO ledger.outbox(topic,payload) VALUES('ledger.recon.failed',$1)`, payload); err != nil {
			return ReconciliationRun{}, err
		}
	}
	summary, err := json.Marshal(counts)
	if err != nil {
		return ReconciliationRun{}, err
	}
	finished := s.now()
	if _, err := tx.Exec(ctx, `UPDATE ledger.recon_run SET status=$2,finished_at=$3,summary=$4 WHERE id=$1`, run.ID, status, finished, summary); err != nil {
		return ReconciliationRun{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ReconciliationRun{}, err
	}
	run.Status, run.FinishedAt, run.Summary, run.Issues = status, &finished, counts, issues
	if s.reconciliationMetrics != nil {
		s.reconciliationMetrics.ReconRun(status == "ok")
		for code, count := range counts {
			s.reconciliationMetrics.ReconMismatch(code, count)
		}
	}
	return run, nil
}

func (s *Service) failReconciliationRun(ctx context.Context, runID int64, _ error) {
	detail, _ := json.Marshal(map[string]int{"EXECUTION_ERROR": 1})
	var id int64
	if err := s.db.QueryRow(ctx, `UPDATE ledger.recon_run SET status='failed',finished_at=$2,summary=$3 WHERE id=$1 RETURNING id`, runID, s.now(), detail).Scan(&id); err == nil && s.reconciliationMetrics != nil {
		s.reconciliationMetrics.ReconRun(false)
	}
}

func (s *Service) loadReconciliationRun(ctx context.Context, db transactionReader, id int64) (ReconciliationRun, error) {
	var run ReconciliationRun
	var summary []byte
	var forceReason *string
	err := db.QueryRow(ctx, `SELECT id,day,kind,attempt,started_by,force_reason,started_at,finished_at,status,summary FROM ledger.recon_run WHERE id=$1`, id).
		Scan(&run.ID, &run.Day, &run.Kind, &run.Attempt, &run.StartedBy, &forceReason, &run.StartedAt, &run.FinishedAt, &run.Status, &summary)
	if err != nil {
		return ReconciliationRun{}, err
	}
	if forceReason != nil {
		run.ForceReason = *forceReason
	}
	if err := json.Unmarshal(summary, &run.Summary); err != nil {
		return ReconciliationRun{}, err
	}
	rows, err := db.Query(ctx, `SELECT id,run_id,check_code,account_id,txn_id,expected,actual,COALESCE(detail,''),created_at FROM ledger.recon_issue WHERE run_id=$1 ORDER BY id`, id)
	if err != nil {
		return ReconciliationRun{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item ReconIssue
		if err := rows.Scan(&item.ID, &item.RunID, &item.CheckCode, &item.AccountID, &item.TxnID, &item.Expected, &item.Actual, &item.Detail, &item.CreatedAt); err != nil {
			return ReconciliationRun{}, err
		}
		run.Issues = append(run.Issues, item)
	}
	return run, rows.Err()
}

func runReconciliationChecks(ctx context.Context, tx pgx.Tx, accountIDs []int64) ([]ReconIssue, error) {
	var issues []ReconIssue
	checks := []func(context.Context, pgx.Tx, []int64) ([]ReconIssue, error){
		checkBalanceCache, checkTxnBalanced, checkIdentity, checkHoldsFrozen,
		checkPendingPayout, checkReversals, checkOrphans,
	}
	for _, check := range checks {
		found, err := check(ctx, tx, accountIDs)
		if err != nil {
			return nil, err
		}
		issues = append(issues, found...)
	}
	return issues, nil
}

func checkBalanceCache(ctx context.Context, tx pgx.Tx, accountIDs []int64) ([]ReconIssue, error) {
	rows, err := tx.Query(ctx, `
		WITH computed AS (
			SELECT a.id,a.normal_side,a.allow_negative,b.allow_negative AS cached_policy,b.balance,b.version,count(p.id)::bigint AS expected_version,
				CASE a.normal_side WHEN 'DEBIT'
				THEN COALESCE(sum(CASE p.side WHEN 'DEBIT' THEN p.amount ELSE -p.amount END),0)
				ELSE COALESCE(sum(CASE p.side WHEN 'CREDIT' THEN p.amount ELSE -p.amount END),0)
				END::bigint AS expected
			FROM ledger.account a JOIN ledger.balance b ON b.account_id=a.id
			LEFT JOIN ledger.posting p ON p.account_id=a.id
			WHERE COALESCE(cardinality($1::bigint[]),0)=0 OR a.id=ANY($1)
			GROUP BY a.id,a.normal_side,a.allow_negative,b.allow_negative,b.balance,b.version
		)
		SELECT id,expected,balance,allow_negative,cached_policy,version,expected_version
		FROM computed
		WHERE expected<>balance OR allow_negative<>cached_policy OR version<>expected_version OR (NOT allow_negative AND balance<0)
		ORDER BY id`, accountIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []ReconIssue
	for rows.Next() {
		var id, expected, actual, version, expectedVersion int64
		var policy, cachedPolicy bool
		if err := rows.Scan(&id, &expected, &actual, &policy, &cachedPolicy, &version, &expectedVersion); err != nil {
			return nil, err
		}
		detail := fmt.Sprintf("posting balance mismatch; policy=%t cached_policy=%t version=%d expected_version=%d", policy, cachedPolicy, version, expectedVersion)
		result = append(result, issue(CheckBalanceCache, &id, nil, expected, actual, detail))
	}
	return result, rows.Err()
}

func checkTxnBalanced(ctx context.Context, tx pgx.Tx, _ []int64) ([]ReconIssue, error) {
	rows, err := tx.Query(ctx, `
		SELECT t.id,t.amount,COALESCE(sum(p.amount) FILTER (WHERE p.side='DEBIT'),0),
			COALESCE(sum(p.amount) FILTER (WHERE p.side='CREDIT'),0),count(p.id)
		FROM ledger.txn t LEFT JOIN ledger.posting p ON p.txn_id=t.id
		GROUP BY t.id,t.amount
		HAVING count(p.id)<2 OR COALESCE(sum(p.amount) FILTER (WHERE p.side='DEBIT'),0)<>t.amount
			OR COALESCE(sum(p.amount) FILTER (WHERE p.side='CREDIT'),0)<>t.amount
		ORDER BY t.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []ReconIssue
	for rows.Next() {
		var id, amount, debit, credit, count int64
		if err := rows.Scan(&id, &amount, &debit, &credit, &count); err != nil {
			return nil, err
		}
		result = append(result, issue(CheckTxnBalanced, nil, &id, amount, debit, fmt.Sprintf("debit=%d credit=%d postings=%d", debit, credit, count)))
	}
	return result, rows.Err()
}

func checkIdentity(ctx context.Context, tx pgx.Tx, _ []int64) ([]ReconIssue, error) {
	var bank, liabilities int64
	if err := tx.QueryRow(ctx, `
		WITH posting_balance AS (
			SELECT a.id,a.kind,
				CASE a.normal_side WHEN 'DEBIT'
					THEN COALESCE(sum(CASE p.side WHEN 'DEBIT' THEN p.amount ELSE -p.amount END),0)
					ELSE COALESCE(sum(CASE p.side WHEN 'CREDIT' THEN p.amount ELSE -p.amount END),0)
				END::bigint AS amount
			FROM ledger.account a LEFT JOIN ledger.posting p ON p.account_id=a.id
			GROUP BY a.id,a.kind,a.normal_side
		)
		SELECT COALESCE(sum(b.balance) FILTER (WHERE a.kind='BANK_DEDICATED'),0),
			COALESCE(sum(pb.amount) FILTER (WHERE pb.kind<>'BANK_DEDICATED'),0)
		FROM ledger.account a JOIN ledger.balance b ON b.account_id=a.id
		JOIN posting_balance pb ON pb.id=a.id`).Scan(&bank, &liabilities); err != nil {
		return nil, err
	}
	if bank == liabilities {
		return nil, nil
	}
	return []ReconIssue{issue(CheckIdentity, nil, nil, bank, liabilities, "non-bank balances must equal dedicated bank balance")}, nil
}

func checkHoldsFrozen(ctx context.Context, tx pgx.Tx, _ []int64) ([]ReconIssue, error) {
	rows, err := tx.Query(ctx, `
		SELECT a.id,COALESCE(sum(h.amount) FILTER (WHERE h.status IN ('held','disputed')),0),b.balance
		FROM ledger.account a JOIN ledger.balance b ON b.account_id=a.id
		LEFT JOIN ledger.hold h ON h.account_id=a.id
		WHERE a.kind='WALLET_FROZEN'
		GROUP BY a.id,b.balance
		HAVING b.balance<>COALESCE(sum(h.amount) FILTER (WHERE h.status IN ('held','disputed')),0)
		ORDER BY a.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []ReconIssue
	for rows.Next() {
		var id, expected, actual int64
		if err := rows.Scan(&id, &expected, &actual); err != nil {
			return nil, err
		}
		result = append(result, issue(CheckHoldFrozen, &id, nil, expected, actual, "open holds must equal frozen wallet balance"))
	}
	return result, rows.Err()
}

func checkPendingPayout(ctx context.Context, tx pgx.Tx, _ []int64) ([]ReconIssue, error) {
	var accountID, expected, actual int64
	err := tx.QueryRow(ctx, `
		SELECT a.id,COALESCE((SELECT sum(amount) FROM ledger.withdrawal WHERE status IN ('requested','review','approved')),0),b.balance
		FROM ledger.account a JOIN ledger.balance b ON b.account_id=a.id
		WHERE a.kind='PENDING_PAYOUT'
		  AND b.balance<>COALESCE((SELECT sum(amount) FROM ledger.withdrawal WHERE status IN ('requested','review','approved')),0)`).Scan(&accountID, &expected, &actual)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return []ReconIssue{issue(CheckPendingPayout, &accountID, nil, expected, actual, "open withdrawals must equal pending payout balance")}, nil
}

func checkReversals(ctx context.Context, tx pgx.Tx, _ []int64) ([]ReconIssue, error) {
	rows, err := tx.Query(ctx, `
		SELECT r.id,r.amount,COALESCE(o.amount,0)
		FROM ledger.txn r LEFT JOIN ledger.txn o ON o.id=r.reverses_txn_id
		WHERE r.type='reversal' AND (
			o.id IS NULL OR r.amount<>o.amount OR
			EXISTS (
				SELECT account_id,CASE side WHEN 'DEBIT' THEN 'CREDIT' ELSE 'DEBIT' END,amount,role,category FROM ledger.posting WHERE txn_id=o.id
				EXCEPT ALL SELECT account_id,side,amount,role,category FROM ledger.posting WHERE txn_id=r.id
			) OR EXISTS (
				SELECT account_id,side,amount,role,category FROM ledger.posting WHERE txn_id=r.id
				EXCEPT ALL SELECT account_id,CASE side WHEN 'DEBIT' THEN 'CREDIT' ELSE 'DEBIT' END,amount,role,category FROM ledger.posting WHERE txn_id=o.id
			)
		) ORDER BY r.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []ReconIssue
	for rows.Next() {
		var id, amount, originalAmount int64
		if err := rows.Scan(&id, &amount, &originalAmount); err != nil {
			return nil, err
		}
		result = append(result, issue(CheckReversal, nil, &id, originalAmount, amount, "reversal must exactly mirror its original transaction"))
	}
	return result, rows.Err()
}

func checkOrphans(ctx context.Context, tx pgx.Tx, _ []int64) ([]ReconIssue, error) {
	rows, err := tx.Query(ctx, `
		SELECT NULL::bigint,p.id,'posting without transaction' FROM ledger.posting p LEFT JOIN ledger.txn t ON t.id=p.txn_id WHERE t.id IS NULL
		UNION ALL
		SELECT t.id,NULL::bigint,'transaction without postings' FROM ledger.txn t LEFT JOIN ledger.posting p ON p.txn_id=t.id WHERE p.id IS NULL
		ORDER BY 1 NULLS LAST,2 NULLS LAST`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []ReconIssue
	for rows.Next() {
		var txnID, postingID *int64
		var detail string
		if err := rows.Scan(&txnID, &postingID, &detail); err != nil {
			return nil, err
		}
		if postingID != nil {
			detail = fmt.Sprintf("%s: posting_id=%d", detail, *postingID)
		}
		result = append(result, issue(CheckOrphan, nil, txnID, 0, 1, detail))
	}
	return result, rows.Err()
}

func issue(code string, accountID, txnID *int64, expected, actual int64, detail string) ReconIssue {
	return ReconIssue{CheckCode: code, AccountID: accountID, TxnID: txnID, Expected: &expected, Actual: &actual, Detail: detail}
}

func countsForAccount(issues []ReconIssue, code string, accountID int64) int {
	count := 0
	for _, item := range issues {
		if item.CheckCode == code && item.AccountID != nil && *item.AccountID == accountID {
			count++
		}
	}
	return count
}

func (s *Service) reconciliationDay(day time.Time) (time.Time, time.Time) {
	local := day.In(s.reconciliationLocation)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, s.reconciliationLocation)
	return start, start.AddDate(0, 0, 1).UTC()
}

func (s *Service) reconciliationDayClosed(day time.Time) bool {
	start, _ := s.reconciliationDay(day)
	today, _ := s.reconciliationDay(s.now())
	return start.Before(today)
}

func checkPayoutFreeze(ctx context.Context, tx pgx.Tx, accountID int64) error {
	var frozen bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM ledger.payout_freeze
		WHERE released_at IS NULL AND (scope='global' OR (scope='account' AND account_id=$1))
	)`, accountID).Scan(&frozen)
	if err != nil {
		return err
	}
	if frozen {
		return ErrPayoutFrozen
	}
	return nil
}
