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
	"github.com/jackc/pgx/v5/pgconn"
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

var (
	// ErrAdminNotFound is returned when the selected administrator does not exist.
	ErrAdminNotFound = errors.New("iam: administrator not found")
	// ErrAdminSelfMutation prevents an administrator from changing their own role or status.
	ErrAdminSelfMutation = errors.New("iam: administrator cannot change own role or status")
	// ErrLastActiveSuper protects against removing the final enabled super administrator.
	ErrLastActiveSuper = errors.New("iam: cannot remove last active super administrator")
)

type adminListCursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        int64     `json:"id"`
}

// AdminManagementService owns sensitive administrator creation. The initial
// password and provisioning URI are returned only to the direct caller and are
// deliberately not persisted in result-like database columns or audit detail.
type AdminManagementService struct {
	DB           *pgxpool.Pool
	Cipher       TOTPSecretCipher
	Audit        platformaudit.Writer
	SessionCache SessionCache
	Now          func() time.Time
}

// DisableWithAudit disables an administrator and immediately revokes all of
// their existing sessions. The database trigger remains the concurrent guard
// against removing the final active super administrator.
func (s *AdminManagementService) DisableWithAudit(ctx context.Context, publicID, reason string, auditInput AdminAuditInput) (AdminUser, error) {
	if strings.TrimSpace(reason) == "" {
		return AdminUser{}, errors.New("iam: administrator disable reason is required")
	}
	return s.changeStatusWithAudit(ctx, publicID, AdminStatus("disabled"), reason, auditInput)
}

// EnableWithAudit enables an administrator. Existing sessions remain revoked;
// the administrator must sign in again.
func (s *AdminManagementService) EnableWithAudit(ctx context.Context, publicID string, auditInput AdminAuditInput) (AdminUser, error) {
	return s.changeStatusWithAudit(ctx, publicID, AdminStatusActive, "", auditInput)
}

func (s *AdminManagementService) changeStatusWithAudit(ctx context.Context, publicID string, status AdminStatus, reason string, auditInput AdminAuditInput) (AdminUser, error) {
	if status != AdminStatusActive && status != AdminStatus("disabled") {
		return AdminUser{}, errors.New("iam: invalid administrator status")
	}
	actorID, actorPublicID, err := s.auditActor(ctx, auditInput)
	if err != nil {
		return AdminUser{}, err
	}
	if actorPublicID == strings.TrimSpace(publicID) {
		return AdminUser{}, ErrAdminSelfMutation
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return AdminUser{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	before, err := scanAdminUser(tx.QueryRow(ctx, `SELECT public_id,name,email,role,data_scope,status,last_login_at,created_at FROM iam.admin_user WHERE public_id=$1 FOR UPDATE`, strings.TrimSpace(publicID)))
	if err != nil {
		return AdminUser{}, adminMutationError(err)
	}
	if err := ensureActiveSuperRemains(ctx, tx, before, before.Role, status); err != nil {
		return AdminUser{}, err
	}
	after, err := scanAdminUser(tx.QueryRow(ctx, `UPDATE iam.admin_user SET status=$1,updated_at=$2 WHERE public_id=$3 RETURNING public_id,name,email,role,data_scope,status,last_login_at,created_at`, status, s.now(), before.PublicID))
	if err != nil {
		return AdminUser{}, adminMutationError(err)
	}
	var revoked []adminSession
	if status != AdminStatusActive {
		rows, err := tx.Query(ctx, `UPDATE iam.session SET revoked_at=$1,revoked_reason='admin_disabled',updated_at=$1 WHERE admin_user_id=(SELECT id FROM iam.admin_user WHERE public_id=$2) AND revoked_at IS NULL RETURNING public_id,expires_at`, s.now(), before.PublicID)
		if err != nil {
			return AdminUser{}, err
		}
		for rows.Next() {
			var item adminSession
			if err := rows.Scan(&item.ID, &item.ExpiresAt); err != nil {
				rows.Close()
				return AdminUser{}, err
			}
			revoked = append(revoked, item)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return AdminUser{}, err
		}
		rows.Close()
	}
	auditEntry, err := s.managementAuditEntry(actorID, actorPublicID, before, after, reason, auditInput)
	if err != nil {
		return AdminUser{}, err
	}
	outboxID, err := queueAdminAudit(ctx, tx, auditEntry)
	if err != nil {
		return AdminUser{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AdminUser{}, err
	}
	s.revokeCachedAdminSessions(ctx, before.PublicID, revoked)
	_ = deliverAdminAudit(ctx, s.DB, s.Audit, outboxID)
	return after, nil
}

// UpdateRoleWithAudit updates role and data scope and revokes all sessions so
// a new access token is required for the changed privileges.
func (s *AdminManagementService) UpdateRoleWithAudit(ctx context.Context, publicID string, role Role, dataScope []string, auditInput AdminAuditInput) (AdminUser, error) {
	if !validAdminRole(role) {
		return AdminUser{}, errors.New("iam: invalid administrator role")
	}
	actorID, actorPublicID, err := s.auditActor(ctx, auditInput)
	if err != nil {
		return AdminUser{}, err
	}
	if actorPublicID == strings.TrimSpace(publicID) {
		return AdminUser{}, ErrAdminSelfMutation
	}
	if dataScope == nil {
		dataScope = []string{}
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return AdminUser{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	before, err := scanAdminUser(tx.QueryRow(ctx, `SELECT public_id,name,email,role,data_scope,status,last_login_at,created_at FROM iam.admin_user WHERE public_id=$1 FOR UPDATE`, strings.TrimSpace(publicID)))
	if err != nil {
		return AdminUser{}, adminMutationError(err)
	}
	if err := ensureActiveSuperRemains(ctx, tx, before, role, before.Status); err != nil {
		return AdminUser{}, err
	}
	after, err := scanAdminUser(tx.QueryRow(ctx, `UPDATE iam.admin_user SET role=$1,data_scope=$2,updated_at=$3 WHERE public_id=$4 RETURNING public_id,name,email,role,data_scope,status,last_login_at,created_at`, role, dataScope, s.now(), before.PublicID))
	if err != nil {
		return AdminUser{}, adminMutationError(err)
	}
	rows, err := tx.Query(ctx, `UPDATE iam.session SET revoked_at=$1,revoked_reason='admin_role_changed',updated_at=$1 WHERE admin_user_id=(SELECT id FROM iam.admin_user WHERE public_id=$2) AND revoked_at IS NULL RETURNING public_id,expires_at`, s.now(), before.PublicID)
	if err != nil {
		return AdminUser{}, err
	}
	var revoked []adminSession
	for rows.Next() {
		var item adminSession
		if err := rows.Scan(&item.ID, &item.ExpiresAt); err != nil {
			rows.Close()
			return AdminUser{}, err
		}
		revoked = append(revoked, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return AdminUser{}, err
	}
	rows.Close()
	auditEntry, err := s.managementAuditEntry(actorID, actorPublicID, before, after, "", auditInput)
	if err != nil {
		return AdminUser{}, err
	}
	outboxID, err := queueAdminAudit(ctx, tx, auditEntry)
	if err != nil {
		return AdminUser{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AdminUser{}, err
	}
	s.revokeCachedAdminSessions(ctx, before.PublicID, revoked)
	_ = deliverAdminAudit(ctx, s.DB, s.Audit, outboxID)
	return after, nil
}

func ensureActiveSuperRemains(ctx context.Context, tx pgx.Tx, before AdminUser, nextRole Role, nextStatus AdminStatus) error {
	if before.Role != RoleSuper || before.Status != AdminStatusActive || (nextRole == RoleSuper && nextStatus == AdminStatusActive) {
		return nil
	}
	var activeSuperCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM iam.admin_user WHERE role='super' AND status='active'`).Scan(&activeSuperCount); err != nil {
		return err
	}
	if activeSuperCount <= 1 {
		return ErrLastActiveSuper
	}
	return nil
}

func adminMutationError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAdminNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "P0001" {
		return ErrLastActiveSuper
	}
	return err
}

type adminSession struct {
	ID        string
	ExpiresAt time.Time
}

func (s *AdminManagementService) revokeCachedAdminSessions(ctx context.Context, adminPublicID string, sessions []adminSession) {
	if s.SessionCache == nil {
		return
	}
	now := s.now()
	for _, session := range sessions {
		_ = s.SessionCache.Revoke(ctx, session.ID, positiveTTL(now, session.ExpiresAt))
	}
}

func (s *AdminManagementService) auditActor(ctx context.Context, input AdminAuditInput) (int64, string, error) {
	if s == nil || s.DB == nil || s.Audit == nil {
		return 0, "", errors.New("iam: administrator management audit unavailable")
	}
	actorPublicID := strings.TrimSpace(input.ActorPublicID)
	if actorPublicID == "" || strings.TrimSpace(input.RequestID) == "" {
		return 0, "", errors.New("iam: administrator management audit context is required")
	}
	var actorID int64
	if err := s.DB.QueryRow(ctx, `SELECT id FROM iam.admin_user WHERE public_id=$1 AND status='active'`, actorPublicID).Scan(&actorID); err != nil {
		return 0, "", err
	}
	return actorID, actorPublicID, nil
}

func (s *AdminManagementService) managementAuditEntry(actorID int64, actorPublicID string, before, after AdminUser, reason string, input AdminAuditInput) (platformaudit.Entry, error) {
	detail, err := json.Marshal(struct {
		ActorPublicID  string    `json:"actor_public_id"`
		TargetPublicID string    `json:"target_public_id"`
		Before         adminView `json:"before"`
		After          adminView `json:"after"`
		Reason         string    `json:"reason,omitempty"`
	}{ActorPublicID: actorPublicID, TargetPublicID: after.PublicID, Before: auditAdminView(before), After: auditAdminView(after), Reason: strings.TrimSpace(reason)})
	if err != nil {
		return platformaudit.Entry{}, err
	}
	return platformaudit.Entry{Time: s.now(), ActorID: actorID, ActorRole: "admin", Action: platformaudit.ActionPermissionChanged, Target: "admin:" + after.PublicID, Detail: detail, IP: input.IP, RequestID: input.RequestID}, nil
}

type adminView struct {
	Role      Role        `json:"role"`
	Status    AdminStatus `json:"status"`
	DataScope []string    `json:"data_scope"`
}

func auditAdminView(user AdminUser) adminView {
	return adminView{Role: user.Role, Status: user.Status, DataScope: user.DataScope}
}

func scanAdminUser(row pgx.Row) (AdminUser, error) {
	var user AdminUser
	err := row.Scan(&user.PublicID, &user.Name, &user.Email, &user.Role, &user.DataScope, &user.Status, &user.LastLoginAt, &user.CreatedAt)
	return user, err
}

func (s *AdminManagementService) Create(ctx context.Context, input AdminCreateInput) (AdminCreateResult, error) {
	prepared, err := s.prepareAdminCreate(ctx, input)
	if err != nil {
		return AdminCreateResult{}, err
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return AdminCreateResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	user, err := insertPreparedAdmin(ctx, tx, prepared)
	if err != nil {
		return AdminCreateResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AdminCreateResult{}, err
	}
	return AdminCreateResult{Admin: user, InitialPassword: prepared.password, TOTPProvisioningURI: prepared.provisioningURI}, nil
}

type preparedAdminCreate struct {
	publicID, name, email, passwordHash, password, provisioningURI string
	totpSecretEnc                                                  []byte
	role                                                           Role
	dataScope                                                      []string
	at                                                             time.Time
}

func (s *AdminManagementService) prepareAdminCreate(ctx context.Context, input AdminCreateInput) (preparedAdminCreate, error) {
	if s == nil || s.DB == nil || s.Cipher == nil || strings.TrimSpace(input.Name) == "" || !validAdminRole(input.Role) {
		return preparedAdminCreate{}, errors.New("iam: invalid administrator creation input")
	}
	email := strings.ToLower(strings.TrimSpace(input.Email))
	if !strings.Contains(email, "@") || len(email) > 320 {
		return preparedAdminCreate{}, errors.New("iam: invalid administrator email")
	}
	publicID, err := ids.NewULID()
	if err != nil {
		return preparedAdminCreate{}, err
	}
	password, err := newInitialAdminPassword()
	if err != nil {
		return preparedAdminCreate{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return preparedAdminCreate{}, err
	}
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "Dr.life+", AccountName: email, SecretSize: 32, Period: 30})
	if err != nil {
		return preparedAdminCreate{}, err
	}
	enc, err := EncryptTOTPSecret(ctx, s.Cipher, publicID, key.Secret())
	if err != nil {
		return preparedAdminCreate{}, err
	}
	if input.DataScope == nil {
		input.DataScope = []string{}
	}
	return preparedAdminCreate{publicID: publicID, name: strings.TrimSpace(input.Name), email: email, passwordHash: hash, password: password, provisioningURI: key.URL(), totpSecretEnc: enc, role: input.Role, dataScope: input.DataScope, at: s.now()}, nil
}

func insertPreparedAdmin(ctx context.Context, tx pgx.Tx, prepared preparedAdminCreate) (AdminUser, error) {
	var user AdminUser
	err := tx.QueryRow(ctx, `
		INSERT INTO iam.admin_user(public_id,name,email,password_hash,totp_secret_enc,role,data_scope,status,password_change_required,created_at,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,'active',true,$8,$8)
		RETURNING public_id,name,email,role,data_scope,status,last_login_at,created_at`,
		prepared.publicID, prepared.name, prepared.email, prepared.passwordHash, prepared.totpSecretEnc, prepared.role, prepared.dataScope, prepared.at,
	).Scan(&user.PublicID, &user.Name, &user.Email, &user.Role, &user.DataScope, &user.Status, &user.LastLoginAt, &user.CreatedAt)
	return user, err
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
	prepared, err := s.prepareAdminCreate(ctx, input)
	if err != nil {
		return AdminCreateResult{}, err
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return AdminCreateResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var actorID int64
	if err := tx.QueryRow(ctx, `SELECT id FROM iam.admin_user WHERE public_id=$1 AND status='active'`, actorPublicID).Scan(&actorID); err != nil {
		return AdminCreateResult{}, err
	}
	createdUser, err := insertPreparedAdmin(ctx, tx, prepared)
	if err != nil {
		return AdminCreateResult{}, err
	}
	created := AdminCreateResult{Admin: createdUser, InitialPassword: prepared.password, TOTPProvisioningURI: prepared.provisioningURI}
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
	outboxID, err := queueAdminAudit(ctx, tx, platformaudit.Entry{
		Time: s.now(), ActorID: actorID, ActorRole: "admin", Action: platformaudit.ActionPermissionChanged,
		Target: "admin:" + created.Admin.PublicID, Detail: detail, IP: auditInput.IP, RequestID: auditInput.RequestID,
	})
	if err != nil {
		return AdminCreateResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AdminCreateResult{}, err
	}
	_ = deliverAdminAudit(ctx, s.DB, s.Audit, outboxID)
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

// Get returns one credential-safe administrator record by its public ID.
func (s *AdminManagementService) Get(ctx context.Context, publicID string) (AdminUser, bool, error) {
	if s == nil || s.DB == nil {
		return AdminUser{}, false, errors.New("iam: administrator management database unavailable")
	}
	user, err := scanAdminUser(s.DB.QueryRow(ctx, `SELECT public_id,name,email,role,data_scope,status,last_login_at,created_at FROM iam.admin_user WHERE public_id=$1`, strings.TrimSpace(publicID)))
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminUser{}, false, nil
	}
	if err != nil {
		return AdminUser{}, false, err
	}
	return user, true, nil
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
