package ledger

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Service) maybeSpotCheck(postings []Posting) {
	if s.spotCheckEvery == 0 || s.spotCheckCounter.Add(1)%s.spotCheckEvery != 0 {
		return
	}
	accountSet := make(map[int64]struct{}, len(postings))
	for _, posting := range postings {
		accountSet[posting.AccountID] = struct{}{}
	}
	accountIDs := make([]int64, 0, len(accountSet))
	for id := range accountSet {
		accountIDs = append(accountIDs, id)
	}
	s.spotCheckMu.Lock()
	if s.spotCheckClosed {
		s.spotCheckMu.Unlock()
		return
	}
	select {
	case s.spotCheckSem <- struct{}{}:
		s.spotCheckWG.Add(1)
		s.spotCheckMu.Unlock()
	default:
		s.spotCheckMu.Unlock()
		if s.reconciliationMetrics != nil {
			s.reconciliationMetrics.ReconSpotCheckDropped()
		}
		return
	}
	go func() {
		defer func() { <-s.spotCheckSem; s.spotCheckWG.Done() }()
		ctx, cancel := context.WithTimeout(s.spotCheckCtx, 30*time.Second)
		defer cancel()
		_ = s.runSpotCheck(ctx, accountIDs)
	}()
}

func (s *Service) runSpotCheck(ctx context.Context, accountIDs []int64) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		s.recordSpotCheckFailure()
		return err
	}
	issues, err := checkBalanceCache(ctx, tx, accountIDs)
	if err == nil {
		err = tx.Commit(ctx)
	} else {
		_ = tx.Rollback(ctx)
	}
	if err != nil {
		s.recordSpotCheckFailure()
		return err
	}
	if len(issues) == 0 {
		return nil
	}
	day, _ := s.reconciliationDay(s.now())
	run, _, err := s.startReconciliationRun(ctx, day, "spot", ReconciliationOptions{Force: true, Reason: "automatic posting spot check"})
	if err != nil {
		s.recordSpotCheckFailure()
		return err
	}
	_, err = s.finishReconciliationRun(ctx, run, issues)
	if err != nil {
		s.failReconciliationRun(ctx, run.ID, err)
	}
	return err
}

func (s *Service) recordSpotCheckFailure() {
	if s.reconciliationMetrics != nil {
		s.reconciliationMetrics.ReconRun(false)
	}
	slog.Error("ledger spot check failed")
}

func (s *Service) StopSpotChecks(ctx context.Context) error {
	s.spotCheckMu.Lock()
	s.spotCheckClosed = true
	s.spotCheckMu.Unlock()
	s.spotCheckCancel()
	done := make(chan struct{})
	go func() { s.spotCheckWG.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
