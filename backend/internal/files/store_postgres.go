package files

import (
	"context"
	"errors"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore is the sole persistence adapter for ops.file.
type PostgresStore struct{ DB *pgxpool.Pool }

func (s PostgresStore) Create(ctx context.Context, file File) error {
	if s.DB == nil {
		return ErrNotConfigured
	}
	_, err := s.DB.Exec(ctx, `INSERT INTO ops.file
		(public_id, owner_account, purpose, object_key, content_type, size, sha256, status, retention_until, created_at, updated_at)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$10)`,
		file.PublicID, file.OwnerAccount, file.Purpose, file.ObjectKey, file.ContentType, file.Size, file.SHA256, file.Status, file.RetentionUntil, file.CreatedAt)
	return err
}

// Cleanup deletes each object while its row is locked, then writes its
// terminal status in the same transaction. A deletion failure rolls the
// transaction back so the object key remains available for retry.
func (s PostgresStore) Cleanup(ctx context.Context, now time.Time, deleteObject func(context.Context, string) error) (CleanupResult, error) {
	if s.DB == nil || deleteObject == nil {
		return CleanupResult{}, ErrNotConfigured
	}
	var result CleanupResult
	for {
		tx, err := s.DB.Begin(ctx)
		if err != nil {
			return result, err
		}
		candidate, terminal, err := cleanupCandidateTx(ctx, tx, now)
		if errors.Is(err, pgx.ErrNoRows) {
			if commitErr := tx.Commit(ctx); commitErr != nil {
				return result, commitErr
			}
			return result, nil
		}
		if err != nil {
			tx.Rollback(ctx)
			return result, err
		}
		if err := deleteObject(ctx, candidate.ObjectKey); err != nil && !errors.Is(err, fs.ErrNotExist) {
			tx.Rollback(ctx)
			return result, err
		}
		if _, err := tx.Exec(ctx, `UPDATE ops.file SET status=$2, updated_at=$3 WHERE public_id=$1`, candidate.PublicID, terminal, now); err != nil {
			tx.Rollback(ctx)
			return result, err
		}
		if err := tx.Commit(ctx); err != nil {
			return result, err
		}
		if terminal == StatusExpired {
			result.Expired++
		} else {
			result.Deleted++
		}
	}
}

func cleanupCandidateTx(ctx context.Context, tx pgx.Tx, now time.Time) (cleanupCandidate, Status, error) {
	var candidate cleanupCandidate
	var terminal Status
	err := tx.QueryRow(ctx, `SELECT public_id, object_key, status, created_at, retention_until,
        CASE WHEN retention_until <= $1 THEN 'deleted' ELSE 'expired' END
        FROM ops.file
        WHERE (status='pending' AND created_at < $1 - interval '24 hours')
           OR (status IN ('pending','ready','rejected') AND retention_until <= $1)
        ORDER BY CASE WHEN retention_until <= $1 THEN 0 ELSE 1 END, created_at
		FOR UPDATE SKIP LOCKED LIMIT 1`, now).Scan(&candidate.PublicID, &candidate.ObjectKey, &candidate.Status, &candidate.CreatedAt, &candidate.RetentionUntil, &terminal)
	return candidate, terminal, err
}

func (s PostgresStore) GetOwned(ctx context.Context, ownerAccount int64, publicID string) (File, error) {
	if s.DB == nil {
		return File{}, ErrNotConfigured
	}
	file, err := scanFile(s.DB.QueryRow(ctx, `SELECT public_id, owner_account, purpose, object_key, content_type, size, sha256, status, created_at
        FROM ops.file WHERE owner_account=$1 AND public_id=$2`, ownerAccount, publicID))
	if errors.Is(err, pgx.ErrNoRows) {
		return File{}, ErrNotFound
	}
	return file, err
}

func (s PostgresStore) MarkVerifying(ctx context.Context, ownerAccount int64, publicID string) (File, error) {
	if s.DB == nil {
		return File{}, ErrNotConfigured
	}
	file, err := scanFile(s.DB.QueryRow(ctx, `UPDATE ops.file SET status='verifying', updated_at=now()
        WHERE owner_account=$1 AND public_id=$2 AND status='pending'
        RETURNING public_id, owner_account, purpose, object_key, content_type, size, sha256, status, created_at`, ownerAccount, publicID))
	if errors.Is(err, pgx.ErrNoRows) {
		return File{}, ErrNotFound
	}
	return file, err
}

func (s PostgresStore) MarkPending(ctx context.Context, ownerAccount int64, publicID string) error {
	if s.DB == nil {
		return ErrNotConfigured
	}
	result, err := s.DB.Exec(ctx, `UPDATE ops.file SET status='pending', updated_at=now()
        WHERE owner_account=$1 AND public_id=$2 AND status='verifying'`, ownerAccount, publicID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
}

func (s PostgresStore) DailyUsage(ctx context.Context, ownerAccount int64, now time.Time) (Usage, error) {
	if s.DB == nil {
		return Usage{}, ErrNotConfigured
	}
	var usage Usage
	err := s.DB.QueryRow(ctx, `SELECT count(*), coalesce(sum(size), 0) FROM ops.file
        WHERE owner_account=$1 AND created_at >= date_trunc('day', $2 AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'
        AND status IN ('pending', 'verifying', 'ready')`, ownerAccount, now).Scan(&usage.Count, &usage.Bytes)
	return usage, err
}

func (s PostgresStore) GetVerifying(ctx context.Context, publicID string) (File, error) {
	if s.DB == nil {
		return File{}, ErrNotConfigured
	}
	file, err := scanFile(s.DB.QueryRow(ctx, `SELECT public_id, owner_account, purpose, object_key, content_type, size, sha256, status, created_at
        FROM ops.file WHERE public_id=$1 AND status='verifying'`, publicID))
	if errors.Is(err, pgx.ErrNoRows) {
		return File{}, ErrNotFound
	}
	return file, err
}

func (s PostgresStore) MarkReady(ctx context.Context, publicID, objectKey string) error {
	if s.DB == nil {
		return ErrNotConfigured
	}
	result, err := s.DB.Exec(ctx, `UPDATE ops.file SET status='ready', object_key=$2, updated_at=now() WHERE public_id=$1 AND status='verifying'`, publicID, objectKey)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
}

func (s PostgresStore) MarkRejected(ctx context.Context, publicID string) error {
	return s.markVerificationStatus(ctx, publicID, StatusRejected)
}

func (s PostgresStore) markVerificationStatus(ctx context.Context, publicID string, status Status) error {
	if s.DB == nil {
		return ErrNotConfigured
	}
	result, err := s.DB.Exec(ctx, `UPDATE ops.file SET status=$2, updated_at=now() WHERE public_id=$1 AND status='verifying'`, publicID, status)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
}

type rowScanner interface{ Scan(...any) error }

func scanFile(row rowScanner) (File, error) {
	var file File
	err := row.Scan(&file.PublicID, &file.OwnerAccount, &file.Purpose, &file.ObjectKey, &file.ContentType, &file.Size, &file.SHA256, &file.Status, &file.CreatedAt)
	return file, err
}
