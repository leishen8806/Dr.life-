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

// Cleanup claims each candidate atomically, releases the row lock, then does
// bounded object-store I/O. A failed deletion clears the claim for retry.
func (s PostgresStore) Cleanup(ctx context.Context, now time.Time, deleteObject func(context.Context, string) error) (CleanupResult, error) {
	if s.DB == nil || deleteObject == nil {
		return CleanupResult{}, ErrNotConfigured
	}
	var result CleanupResult
	for {
		candidate, terminal, err := s.claimCleanup(ctx, now)
		if errors.Is(err, pgx.ErrNoRows) {
			return result, nil
		}
		if err != nil {
			return result, err
		}
		deleteCtx, cancel := context.WithTimeout(ctx, deleteTimeout)
		deleteErr := deleteObject(deleteCtx, candidate.ObjectKey)
		cancel()
		if deleteErr != nil && !errors.Is(deleteErr, fs.ErrNotExist) {
			_, _ = s.DB.Exec(ctx, `UPDATE ops.file SET cleanup_started_at=NULL WHERE public_id=$1 AND cleanup_started_at=$2`, candidate.PublicID, now)
			return result, deleteErr
		}
		resultTag, err := s.DB.Exec(ctx, `UPDATE ops.file SET status=$2, cleanup_started_at=NULL, updated_at=$3
			WHERE public_id=$1 AND cleanup_started_at=$3`, candidate.PublicID, terminal, now)
		if err != nil {
			return result, err
		}
		if resultTag.RowsAffected() != 1 {
			return result, errors.New("file cleanup claim was lost")
		}
		if terminal == StatusExpired {
			result.Expired++
		} else {
			result.Deleted++
		}
	}
}

func (s PostgresStore) claimCleanup(ctx context.Context, now time.Time) (cleanupCandidate, Status, error) {
	var candidate cleanupCandidate
	var terminal Status
	err := s.DB.QueryRow(ctx, `WITH candidate AS (
		SELECT f.id FROM ops.file f
		WHERE ((f.status IN ('pending','verifying') AND f.created_at < $1::timestamptz - interval '24 hours')
		   OR (f.status IN ('ready','rejected') AND f.retention_until <= $1::timestamptz))
		  AND (f.cleanup_started_at IS NULL OR f.cleanup_started_at < $1::timestamptz - interval '10 minutes')
		  AND NOT EXISTS (SELECT 1 FROM ops.file_reference r WHERE r.file_id=f.id AND r.released_at IS NULL)
		ORDER BY CASE WHEN f.retention_until <= $1::timestamptz THEN 0 ELSE 1 END, f.created_at
		FOR UPDATE SKIP LOCKED LIMIT 1
	)
	UPDATE ops.file f SET cleanup_started_at=$1 FROM candidate c WHERE f.id=c.id
	RETURNING f.public_id, f.object_key, f.status, f.created_at, f.retention_until,
		CASE WHEN f.status IN ('pending','verifying') THEN 'expired' ELSE 'deleted' END`, now).Scan(&candidate.PublicID, &candidate.ObjectKey, &candidate.Status, &candidate.CreatedAt, &candidate.RetentionUntil, &terminal)
	return candidate, terminal, err
}

// AttachReference protects a ready file from cleanup while a business record uses it.
func (s PostgresStore) AttachReference(ctx context.Context, filePublicID, referencePublicID string) error {
	if s.DB == nil || filePublicID == "" || referencePublicID == "" {
		return ErrNotConfigured
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var fileID int64
	if err := tx.QueryRow(ctx, `UPDATE ops.file SET retention_until=NULL, updated_at=now()
		WHERE public_id=$1 AND status='ready' AND cleanup_started_at IS NULL RETURNING id`, filePublicID).Scan(&fileID); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotReady
	} else if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ops.file_reference(file_id,reference_public_id)
		VALUES($1,$2) ON CONFLICT(file_id,reference_public_id) DO UPDATE SET released_at=NULL,updated_at=now()`, fileID, referencePublicID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ReleaseReference starts the purpose-specific retention period once the last
// active business reference is released.
func (s PostgresStore) ReleaseReference(ctx context.Context, filePublicID, referencePublicID string, releasedAt time.Time) error {
	if s.DB == nil || filePublicID == "" || referencePublicID == "" || releasedAt.IsZero() {
		return ErrNotConfigured
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `UPDATE ops.file_reference r SET released_at=$3,updated_at=$3
		FROM ops.file f WHERE r.file_id=f.id AND f.public_id=$1 AND r.reference_public_id=$2 AND r.released_at IS NULL`, filePublicID, referencePublicID, releasedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx, `UPDATE ops.file f SET retention_until=$2::timestamptz + CASE f.purpose
		WHEN 'product_image' THEN interval '90 days'
		WHEN 'inbound_photo' THEN interval '2 years'
		WHEN 'delivery_proof' THEN interval '2 years'
		WHEN 'merchant_kyc' THEN interval '5 years' END, updated_at=$2
		WHERE f.public_id=$1 AND NOT EXISTS (
			SELECT 1 FROM ops.file_reference r WHERE r.file_id=f.id AND r.released_at IS NULL
		)`, filePublicID, releasedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
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

func (s PostgresStore) MarkReady(ctx context.Context, publicID, objectKey string, size int64, sha256 []byte) error {
	if s.DB == nil {
		return ErrNotConfigured
	}
	result, err := s.DB.Exec(ctx, `UPDATE ops.file SET status='ready', object_key=$2, size=$3, sha256=$4, updated_at=now() WHERE public_id=$1 AND status='verifying'`, publicID, objectKey, size, sha256)
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
