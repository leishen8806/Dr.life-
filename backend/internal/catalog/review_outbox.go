package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	platformaudit "github.com/leishen8806/Dr.life-/backend/internal/platform/audit"
)

// DeliverReviewAudit delivers one pending catalog review audit row. The
// catalog outbox is the durable boundary; the platform audit append happens
// before the row is marked delivered, and IdempotentWriter prevents duplicate
// hash-chain entries after a worker retry.
func (s *PgStore) DeliverReviewAudit(ctx context.Context, writer platformaudit.Writer) error {
	if s == nil || s.db == nil || writer == nil {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		publicID, actorRole, target, requestID, ip string
		occurredAt                                 time.Time
		actorID                                    int64
		action                                     platformaudit.Action
		detail                                     []byte
	)
	err = tx.QueryRow(ctx, `
		SELECT public_id, occurred_at, actor_id, actor_role, action, target, detail,
		       COALESCE(host(ip), ''), request_id
		FROM catalog.product_review_audit_outbox
		WHERE delivered_at IS NULL
		ORDER BY id
		LIMIT 1
		FOR UPDATE SKIP LOCKED`).Scan(&publicID, &occurredAt, &actorID, &actorRole, &action, &target, &detail, &ip, &requestID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	entry := platformaudit.Entry{Time: occurredAt, ActorID: actorID, ActorRole: actorRole, Action: action, Target: target, Detail: json.RawMessage(detail), IP: ip, RequestID: requestID}
	if idempotent, ok := writer.(platformaudit.IdempotentWriter); ok {
		_, err = idempotent.AppendOnce(ctx, publicID, entry)
	} else {
		_, err = writer.Append(ctx, entry)
	}
	if err != nil {
		// Do not persist provider response bodies or other sensitive values.
		_, _ = tx.Exec(ctx, `UPDATE catalog.product_review_audit_outbox SET attempts=attempts+1,last_error=$2 WHERE public_id=$1`, publicID, "audit append failed")
		_ = tx.Commit(ctx)
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE catalog.product_review_audit_outbox SET delivered_at=clock_timestamp(),attempts=attempts+1,last_error=NULL WHERE public_id=$1`, publicID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RunReviewAuditOutbox retries catalog review audit records until shutdown.
func RunReviewAuditOutbox(ctx context.Context, store *PgStore, writer platformaudit.Writer) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		if store != nil && writer != nil {
			for i := 0; i < 50; i++ {
				if err := store.DeliverReviewAudit(ctx, writer); err != nil {
					break
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
