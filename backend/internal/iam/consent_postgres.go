package iam

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresConsentRepository struct{ DB *pgxpool.Pool }

func (r PostgresConsentRepository) Current(ctx context.Context, kind LegalDocumentKind, at time.Time) (LegalDocument, error) {
	var document LegalDocument
	err := r.DB.QueryRow(ctx, `
		SELECT kind, version, effective_at, body_zh, body_en, body_km
		FROM ops.legal_doc
		WHERE kind=$1 AND published_at IS NOT NULL AND effective_at <= $2
		ORDER BY effective_at DESC, id DESC LIMIT 1`, kind, at).
		Scan(&document.Kind, &document.Version, &document.EffectiveAt, &document.BodyZH, &document.BodyEN, &document.BodyKM)
	if errors.Is(err, pgx.ErrNoRows) {
		return LegalDocument{}, NoPublishedLegalDocumentError{Kind: kind}
	}
	return document, err
}

func (r PostgresConsentRepository) AcceptCurrent(ctx context.Context, accountID string, kind LegalDocumentKind, version, channel string, acceptedAt time.Time) error {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('ops.legal_doc:' || $1))`, kind); err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `
		INSERT INTO ops.consent (account_id, kind, version, accepted_at, channel)
		SELECT a.id, $2, $3, $4, $5 FROM iam.account a
		WHERE a.public_id=$1 AND EXISTS (
			SELECT 1 FROM (
				SELECT version FROM ops.legal_doc
				WHERE kind=$2 AND published_at IS NOT NULL AND effective_at <= $4
				ORDER BY effective_at DESC, id DESC LIMIT 1
			) current WHERE current.version=$3
		)`, accountID, kind, version, acceptedAt, channel)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrConsentVersionNotCurrent
	}
	return tx.Commit(ctx)
}

func (r PostgresConsentRepository) Revoke(ctx context.Context, accountID string, kind LegalDocumentKind, revokedAt time.Time) error {
	_, err := r.DB.Exec(ctx, `
		UPDATE ops.consent c SET revoked_at=$3
		FROM iam.account a
		WHERE c.account_id=a.id AND a.public_id=$1 AND c.kind=$2 AND c.revoked_at IS NULL`, accountID, kind, revokedAt)
	return err
}

func (r PostgresConsentRepository) HasActive(ctx context.Context, accountID string, kind LegalDocumentKind, version string) (bool, error) {
	var accepted bool
	err := r.DB.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM ops.consent c JOIN iam.account a ON a.id=c.account_id
			WHERE a.public_id=$1 AND c.kind=$2 AND c.version=$3 AND c.revoked_at IS NULL
		)`, accountID, kind, version).Scan(&accepted)
	return accepted, err
}
