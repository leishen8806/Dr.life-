package iam

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AdminPermissionStore is the production adapter for API authorization. It
// reads IAM-owned administrator records and the approved role matrix; callers
// receive false for unknown, disabled, or unpermitted administrators.
type AdminPermissionStore struct {
	DB *pgxpool.Pool
}

func (s AdminPermissionStore) Can(ctx context.Context, adminPublicID, module, required string) (bool, error) {
	if s.DB == nil || adminPublicID == "" || !validAdminModule(Module(module)) || !validPermission(Permission(required)) {
		return false, nil
	}
	var allowed bool
	err := s.DB.QueryRow(ctx, `
		SELECT CASE p.level
			WHEN 'edit' THEN 2
			WHEN 'view' THEN 1
			ELSE 0
		END >= CASE $3
			WHEN 'edit' THEN 2
			WHEN 'view' THEN 1
			ELSE 0
		END
		FROM iam.admin_user a
		JOIN iam.admin_role_perm p ON p.role=a.role AND p.module=$2
		WHERE a.public_id=$1 AND a.status='active'`, adminPublicID, module, required).Scan(&allowed)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return allowed, err
}

func (s AdminPermissionStore) HasDataScope(ctx context.Context, adminPublicID string) (bool, error) {
	if s.DB == nil || adminPublicID == "" {
		return false, nil
	}
	var scoped bool
	err := s.DB.QueryRow(ctx, `SELECT cardinality(data_scope)>0 FROM iam.admin_user WHERE public_id=$1 AND status='active'`, adminPublicID).Scan(&scoped)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return scoped, err
}

func validAdminModule(module Module) bool {
	for _, value := range adminModules {
		if module == value {
			return true
		}
	}
	return false
}

func validPermission(permission Permission) bool {
	return permission == PermissionView || permission == PermissionEdit
}
