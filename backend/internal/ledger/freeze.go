package ledger

import (
	"context"
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
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = s.runSpotCheck(ctx, accountIDs)
	}()
}

func (s *Service) runSpotCheck(ctx context.Context, accountIDs []int64) error {
	day, _ := s.reconciliationDay(s.now())
	run, _, err := s.startReconciliationRun(ctx, day, "spot", ReconciliationOptions{Force: true, Reason: "automatic posting spot check"})
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		s.failReconciliationRun(ctx, run.ID, err)
		return err
	}
	issues, err := checkBalanceCache(ctx, tx, accountIDs)
	if err == nil {
		err = tx.Commit(ctx)
	} else {
		_ = tx.Rollback(ctx)
	}
	if err != nil {
		s.failReconciliationRun(ctx, run.ID, err)
		return err
	}
	_, err = s.finishReconciliationRun(ctx, run, issues)
	if err != nil {
		s.failReconciliationRun(ctx, run.ID, err)
	}
	return err
}
