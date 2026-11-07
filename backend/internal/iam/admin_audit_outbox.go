package iam

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	platformaudit "github.com/leishen8806/Dr.life-/backend/internal/platform/audit"
	platformids "github.com/leishen8806/Dr.life-/backend/internal/platform/ids"
)

func enqueueAdminAudit(ctx context.Context, db *pgxpool.Pool, entry platformaudit.Entry) (string, error) {
	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	publicID, err := queueAdminAudit(ctx, tx, entry)
	if err != nil {
		return "", err
	}
	return publicID, tx.Commit(ctx)
}

func queueAdminAudit(ctx context.Context, tx pgx.Tx, entry platformaudit.Entry) (string, error) {
	publicID, err := platformids.NewULID()
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO iam.admin_audit_outbox(public_id,occurred_at,actor_id,actor_role,action,target,detail,ip,request_id)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, publicID, entry.Time, entry.ActorID, entry.ActorRole, entry.Action, entry.Target, entry.Detail, nullableIP(entry.IP), entry.RequestID)
	return publicID, err
}

func deliverAdminAudit(ctx context.Context, db *pgxpool.Pool, writer platformaudit.Writer, publicID string) error {
	if db == nil || writer == nil || publicID == "" {
		return nil
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var entry platformaudit.Entry
	var detail []byte
	err = tx.QueryRow(ctx, `
		SELECT occurred_at,actor_id,actor_role,action,target,detail,COALESCE(host(ip),''),request_id
		FROM iam.admin_audit_outbox WHERE public_id=$1 AND delivered_at IS NULL
		FOR UPDATE SKIP LOCKED`, publicID).
		Scan(&entry.Time, &entry.ActorID, &entry.ActorRole, &entry.Action, &entry.Target, &detail, &entry.IP, &entry.RequestID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	entry.Detail = json.RawMessage(detail)
	if idempotent, ok := writer.(platformaudit.IdempotentWriter); ok {
		_, err = idempotent.AppendOnce(ctx, publicID, entry)
	} else {
		_, err = writer.Append(ctx, entry)
	}
	if err != nil {
		_, _ = tx.Exec(ctx, `UPDATE iam.admin_audit_outbox SET attempts=attempts+1,last_error=$2 WHERE public_id=$1`, publicID, err.Error())
		_ = tx.Commit(ctx)
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE iam.admin_audit_outbox SET delivered_at=clock_timestamp(),attempts=attempts+1,last_error=NULL WHERE public_id=$1`, publicID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RunAdminAuditOutbox retries durable administrator audit records until shutdown.
func RunAdminAuditOutbox(ctx context.Context, db *pgxpool.Pool, writer platformaudit.Writer) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		flushAdminAuditOutbox(ctx, db, writer)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func flushAdminAuditOutbox(ctx context.Context, db *pgxpool.Pool, writer platformaudit.Writer) {
	if db == nil || writer == nil {
		return
	}
	rows, err := db.Query(ctx, `SELECT public_id FROM iam.admin_audit_outbox WHERE delivered_at IS NULL ORDER BY id LIMIT 50`)
	if err != nil {
		return
	}
	var publicIDs []string
	for rows.Next() {
		var publicID string
		if rows.Scan(&publicID) == nil {
			publicIDs = append(publicIDs, publicID)
		}
	}
	rows.Close()
	for _, publicID := range publicIDs {
		_ = deliverAdminAudit(ctx, db, writer, publicID)
	}
	_, _ = db.Exec(ctx, `DELETE FROM iam.admin_audit_outbox WHERE id IN (
		SELECT id FROM iam.admin_audit_outbox WHERE delivered_at < clock_timestamp()-interval '30 days' ORDER BY id LIMIT 1000
	)`)
}
