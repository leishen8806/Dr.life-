package iam

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	platformaudit "github.com/leishen8806/Dr.life-/backend/internal/platform/audit"
	platformids "github.com/leishen8806/Dr.life-/backend/internal/platform/ids"
)

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
	var entry platformaudit.Entry
	var detail []byte
	err := db.QueryRow(ctx, `
		SELECT occurred_at,actor_id,actor_role,action,target,detail,COALESCE(host(ip),''),request_id
		FROM iam.admin_audit_outbox WHERE public_id=$1 AND delivered_at IS NULL`, publicID).
		Scan(&entry.Time, &entry.ActorID, &entry.ActorRole, &entry.Action, &entry.Target, &detail, &entry.IP, &entry.RequestID)
	if err != nil {
		return err
	}
	entry.Detail = json.RawMessage(detail)
	if _, err := writer.Append(ctx, entry); err != nil {
		_, _ = db.Exec(ctx, `UPDATE iam.admin_audit_outbox SET attempts=attempts+1,last_error=$2 WHERE public_id=$1`, publicID, err.Error())
		return err
	}
	_, err = db.Exec(ctx, `UPDATE iam.admin_audit_outbox SET delivered_at=clock_timestamp(),attempts=attempts+1,last_error=NULL WHERE public_id=$1`, publicID)
	return err
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
}
