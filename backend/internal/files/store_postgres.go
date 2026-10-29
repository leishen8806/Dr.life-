package files

import (
	"context"
	"errors"
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
        (public_id, owner_account, purpose, object_key, content_type, size, sha256, status, created_at, updated_at)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$9)`,
		file.PublicID, file.OwnerAccount, file.Purpose, file.ObjectKey, file.ContentType, file.Size, file.SHA256, file.Status, file.CreatedAt)
	return err
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

type rowScanner interface{ Scan(...any) error }

func scanFile(row rowScanner) (File, error) {
	var file File
	err := row.Scan(&file.PublicID, &file.OwnerAccount, &file.Purpose, &file.ObjectKey, &file.ContentType, &file.Size, &file.SHA256, &file.Status, &file.CreatedAt)
	return file, err
}
