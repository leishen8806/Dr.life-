package iam

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	platformaudit "github.com/leishen8806/Dr.life-/backend/internal/platform/audit"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/ids"
	"github.com/pquerna/otp/totp"
)

// AdminUser is the credential-safe representation used by administrator
// management. Hashes and encrypted TOTP values never leave IAM.
type AdminUser struct {
	PublicID    string
	Name        string
	Email       string
	Role        Role
	DataScope   []string
	Status      AdminStatus
	LastLoginAt *time.Time
	CreatedAt   time.Time
}

type AdminCreateInput struct {
	Name      string
	Email     string
	Role      Role
	DataScope []string
}

type AdminCreateResult struct {
	Admin               AdminUser
	InitialPassword     string
	TOTPProvisioningURI string
}

// AdminAuditInput identifies the authenticated administrator and request that
// caused a management write. It deliberately contains no credentials.
type AdminAuditInput struct {
	ActorPublicID string
	IP            string
	RequestID     string
}

type adminListCursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        int64     `json:"id"`
}

// AdminManagementService owns sensitive administrator creation. The initial
// password and provisioning URI are returned only to the direct caller and are
// deliberately not persisted in result-like database columns or audit detail.
type AdminManagementService struct {
	DB     *pgxpool.Pool
	Cipher TOTPSecretCipher
	Audit  platformaudit.Writer
	Now    func() time.Time
}

func (s *AdminManagementService) Create(ctx context.Context, input AdminCreateInput) (AdminCreateResult, error) {
	if s == nil || s.DB == nil || s.Cipher == nil || strings.TrimSpace(input.Name) == "" || !validAdminRole(input.Role) {
		return AdminCreateResult{}, errors.New("iam: invalid administrator creation input")
	}
	email := strings.ToLower(strings.TrimSpace(input.Email))
	if !strings.Contains(email, "@") || len(email) > 320 {
		return AdminCreateResult{}, errors.New("iam: invalid administrator email")
	}
	publicID, err := ids.NewULID()
	if err != nil {
		return AdminCreateResult{}, err
	}
	password, err := newInitialAdminPassword()
	if err != nil {
		return AdminCreateResult{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return AdminCreateResult{}, err
	}
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "Dr.life+", AccountName: email, SecretSize: 32, Period: 30})
	if err != nil {
		return AdminCreateResult{}, err
	}
	secret := key.Secret()
	enc, err := EncryptTOTPSecret(ctx, s.Cipher, publicID, secret)
	if err != nil {
		return AdminCreateResult{}, err
	}

	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return AdminCreateResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var user AdminUser
	err = tx.QueryRow(ctx, `
		INSERT INTO iam.admin_user(public_id,name,email,password_hash,totp_secret_enc,role,data_scope,status,password_change_required,created_at,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,'active',true,$8,$8)
		RETURNING public_id,name,email,role,data_scope,status,last_login_at,created_at`,
		publicID, strings.TrimSpace(input.Name), email, hash, enc, input.Role, input.DataScope, s.now(),
	).Scan(&user.PublicID, &user.Name, &user.Email, &user.Role, &user.DataScope, &user.Status, &user.LastLoginAt, &user.CreatedAt)
	if err != nil {
		return AdminCreateResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AdminCreateResult{}, err
	}
	return AdminCreateResult{Admin: user, InitialPassword: password, TOTPProvisioningURI: key.URL()}, nil
}

// CreateWithAudit creates an administrator and records the permitted metadata
// for the management write. Bootstrap credentials remain only in the result.
func (s *AdminManagementService) CreateWithAudit(ctx context.Context, input AdminCreateInput, auditInput AdminAuditInput) (AdminCreateResult, error) {
	if s == nil || s.DB == nil || s.Audit == nil {
		return AdminCreateResult{}, errors.New("iam: administrator creation audit unavailable")
	}
	actorPublicID := strings.TrimSpace(auditInput.ActorPublicID)
	if actorPublicID == "" || strings.TrimSpace(auditInput.RequestID) == "" {
		return AdminCreateResult{}, errors.New("iam: administrator creation audit context is required")
	}
	var actorID int64
	if err := s.DB.QueryRow(ctx, `SELECT id FROM iam.admin_user WHERE public_id=$1 AND status='active'`, actorPublicID).Scan(&actorID); err != nil {
		return AdminCreateResult{}, err
	}

	created, err := s.Create(ctx, input)
	if err != nil {
		return AdminCreateResult{}, err
	}
	detail, err := json.Marshal(struct {
		ActorPublicID  string `json:"actor_public_id"`
		TargetPublicID string `json:"target_public_id"`
		Before         any    `json:"before"`
		After          struct {
			Role      Role        `json:"role"`
			Status    AdminStatus `json:"status"`
			DataScope []string    `json:"data_scope"`
		} `json:"after"`
	}{
		ActorPublicID: actorPublicID, TargetPublicID: created.Admin.PublicID,
		After: struct {
			Role      Role        `json:"role"`
			Status    AdminStatus `json:"status"`
			DataScope []string    `json:"data_scope"`
		}{Role: created.Admin.Role, Status: created.Admin.Status, DataScope: created.Admin.DataScope},
	})
	if err != nil {
		return AdminCreateResult{}, err
	}
	if _, err := s.Audit.Append(ctx, platformaudit.Entry{
		Time: s.now(), ActorID: actorID, ActorRole: "admin", Action: platformaudit.ActionPermissionChanged,
		Target: "admin:" + created.Admin.PublicID, Detail: detail, IP: auditInput.IP, RequestID: auditInput.RequestID,
	}); err != nil {
		return AdminCreateResult{}, err
	}
	return created, nil
}

// List returns credential-safe administrator records using the (created_at,id)
// keyset required by the administrator management API.
func (s *AdminManagementService) List(ctx context.Context, cursor string, limit int) ([]AdminUser, string, error) {
	if s == nil || s.DB == nil {
		return nil, "", errors.New("iam: administrator management database unavailable")
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	var after adminListCursor
	var afterSet bool
	if cursor != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || json.Unmarshal(decoded, &after) != nil || after.ID <= 0 || after.CreatedAt.IsZero() {
			return nil, "", errors.New("iam: invalid administrator cursor")
		}
		afterSet = true
	}

	rows, err := s.DB.Query(ctx, `
		SELECT id,public_id,name,email,role,data_scope,status,last_login_at,created_at
		FROM iam.admin_user
		WHERE NOT $1::boolean OR (created_at,id) > ($2,$3)
		ORDER BY created_at,id
		LIMIT $4`, afterSet, after.CreatedAt, after.ID, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	type listedAdmin struct {
		AdminUser
		id int64
	}
	listed := make([]listedAdmin, 0, limit+1)
	for rows.Next() {
		var item listedAdmin
		if err := rows.Scan(&item.id, &item.PublicID, &item.Name, &item.Email, &item.Role, &item.DataScope, &item.Status, &item.LastLoginAt, &item.CreatedAt); err != nil {
			return nil, "", err
		}
		listed = append(listed, item)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	next := ""
	if len(listed) > limit {
		last := listed[limit-1]
		encoded, err := json.Marshal(adminListCursor{CreatedAt: last.CreatedAt, ID: last.id})
		if err != nil {
			return nil, "", err
		}
		next = base64.RawURLEncoding.EncodeToString(encoded)
		listed = listed[:limit]
	}
	items := make([]AdminUser, 0, len(listed))
	for _, item := range listed {
		items = append(items, item.AdminUser)
	}
	return items, next, nil
}

func (s *AdminManagementService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func validAdminRole(role Role) bool {
	for _, allowed := range adminRoles {
		if role == allowed {
			return true
		}
	}
	return false
}

func newInitialAdminPassword() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
