package iam

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	platformaudit "github.com/leishen8806/Dr.life-/backend/internal/platform/audit"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/ids"
)

const bootstrapAdvisoryLock int64 = 0x44524c4946455f41

// BootstrapService contains the one-shot operations used by admininit. It is
// deliberately a small command-facing surface over the existing IAM helpers.
type BootstrapService struct {
	DB     *pgxpool.Pool
	Cipher TOTPSecretCipher
	Audit  *platformaudit.Store
}

type BootstrapAdminResult struct {
	Admin               AdminUser
	InitialPassword     string
	TOTPProvisioningURI string
}

// CreateFirstSuper creates the first active super administrator. The advisory
// transaction lock makes two concurrent invocations deterministic without a
// migration or a process-local mutex.
func (s *BootstrapService) CreateFirstSuper(ctx context.Context, username, email string) (BootstrapAdminResult, error) {
	username = strings.TrimSpace(username)
	if s == nil || s.DB == nil || s.Cipher == nil || s.Audit == nil || username == "" || strings.ContainsAny(username, "\r\n") {
		return BootstrapAdminResult{}, errors.New("iam: invalid bootstrap configuration")
	}
	input := AdminCreateInput{Name: username, Email: email, Role: RoleSuper}
	prepared, err := (&AdminManagementService{DB: s.DB, Cipher: s.Cipher}).prepareAdminCreate(ctx, input)
	if err != nil {
		return BootstrapAdminResult{}, err
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return BootstrapAdminResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, bootstrapAdvisoryLock); err != nil {
		return BootstrapAdminResult{}, err
	}
	var active int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM iam.admin_user WHERE role='super' AND status='active'`).Scan(&active); err != nil {
		return BootstrapAdminResult{}, err
	}
	if active != 0 {
		return BootstrapAdminResult{}, errors.New("iam: administrator already initialized; use the admin console")
	}
	created, err := insertPreparedAdmin(ctx, tx, prepared)
	if err != nil {
		return BootstrapAdminResult{}, err
	}
	var actorID int64
	if err := tx.QueryRow(ctx, `SELECT id FROM iam.admin_user WHERE public_id=$1`, created.PublicID).Scan(&actorID); err != nil {
		return BootstrapAdminResult{}, err
	}
	detail, _ := json.Marshal(struct {
		Account string `json:"account"`
		Role    Role   `json:"role"`
	}{created.Name, created.Role})
	if _, err := s.Audit.AppendTx(ctx, tx, platformaudit.Entry{ActorID: actorID, ActorRole: "system_bootstrap", Action: platformaudit.ActionAdminBootstrap, Target: "admin:" + created.PublicID, Detail: detail, RequestID: "admininit:" + created.PublicID}); err != nil {
		return BootstrapAdminResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return BootstrapAdminResult{}, err
	}
	return BootstrapAdminResult{Admin: created, InitialPassword: prepared.password, TOTPProvisioningURI: prepared.provisioningURI}, nil
}

// FixSuperEmail repairs the placeholder address written by older bootstrap
// versions. It is intentionally narrow and idempotent: only an active super
// whose name matches and whose current email is the legacy placeholder can be
// changed. The audit entry is committed in the same transaction as the email.
func (s *BootstrapService) FixSuperEmail(ctx context.Context, username, email string) error {
	username = strings.TrimSpace(username)
	normalizedEmail, err := normalizeAdminEmail(email)
	if err != nil {
		return err
	}
	if s == nil || s.DB == nil || s.Audit == nil || username == "" || strings.ContainsAny(username, "\r\n") {
		return errors.New("iam: invalid bootstrap configuration")
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, bootstrapAdvisoryLock); err != nil {
		return err
	}
	var admin AdminUser
	if err := tx.QueryRow(ctx, `SELECT public_id,name,email,role,data_scope,status,last_login_at,created_at FROM iam.admin_user WHERE name=$1 AND role='super' AND status='active' FOR UPDATE`, username).Scan(&admin.PublicID, &admin.Name, &admin.Email, &admin.Role, &admin.DataScope, &admin.Status, &admin.LastLoginAt, &admin.CreatedAt); err != nil {
		return errors.New("iam: active super administrator not found")
	}
	if admin.Email == normalizedEmail {
		return nil
	}
	if !strings.HasSuffix(strings.ToLower(admin.Email), "@bootstrap.invalid") {
		return errors.New("iam: administrator email is not a bootstrap placeholder")
	}
	var duplicate bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM iam.admin_user WHERE email=$1 AND public_id<>$2)`, normalizedEmail, admin.PublicID).Scan(&duplicate); err != nil {
		return err
	}
	if duplicate {
		return errors.New("iam: administrator email is already in use")
	}
	if _, err := tx.Exec(ctx, `UPDATE iam.admin_user SET email=$1,updated_at=now() WHERE public_id=$2`, normalizedEmail, admin.PublicID); err != nil {
		return err
	}
	var actorID int64
	if err := tx.QueryRow(ctx, `SELECT id FROM iam.admin_user WHERE public_id=$1`, admin.PublicID).Scan(&actorID); err != nil {
		return err
	}
	detail := []byte(`{"message":"email 已修正"}`)
	if _, err := s.Audit.AppendTx(ctx, tx, platformaudit.Entry{ActorID: actorID, ActorRole: "system_bootstrap", Action: platformaudit.ActionAdminBootstrap, Target: "admin:" + admin.PublicID, Detail: detail, RequestID: "admininit:super-email:" + admin.PublicID}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type BootstrapBotInput struct {
	End      string
	Username string
	Actor    string
	Token    string
}

// RegisterBot stores an encrypted bot token and webhook secret. The token is
// read by the command from stdin and is never included in an audit detail.
func (s *BootstrapService) RegisterBot(ctx context.Context, input BootstrapBotInput) error {
	input.End, input.Username, input.Actor, input.Token = strings.TrimSpace(input.End), strings.TrimSpace(input.Username), strings.TrimSpace(input.Actor), strings.TrimSpace(input.Token)
	if s == nil || s.DB == nil || s.Cipher == nil || s.Audit == nil || (input.End != "buyer" && input.End != "merchant") || input.Username == "" || input.Actor == "" || input.Token == "" {
		return errors.New("iam: invalid bot bootstrap input")
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, bootstrapAdvisoryLock); err != nil {
		return err
	}
	var actorID int64
	if err := tx.QueryRow(ctx, `SELECT id FROM iam.admin_user WHERE public_id=$1 AND role='super' AND status='active'`, input.Actor).Scan(&actorID); err != nil {
		return errors.New("iam: active super administrator is required")
	}
	var maxPriority int
	if err := tx.QueryRow(ctx, `SELECT coalesce(max(priority),-1) FROM iam.bot WHERE "end"=$1`, input.End).Scan(&maxPriority); err != nil {
		return err
	}
	var primary bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM iam.bot WHERE "end"=$1 AND role_in_pool='primary')`, input.End).Scan(&primary); err != nil {
		return err
	}
	role, status := "standby", "standby"
	if !primary {
		role, status = "primary", "active"
	}
	publicID, err := ids.NewULID()
	if err != nil {
		return err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return err
	}
	defer wipe(secret)
	webhook := base64.RawURLEncoding.EncodeToString(secret)
	placeholder := []byte{0}
	var id int64
	if err := tx.QueryRow(ctx, `INSERT INTO iam.bot(public_id,"end",username,role_in_pool,status,token_enc,token_last4,webhook_secret_enc,priority) VALUES($1,$2,$3,$4,$5,$6,$7,$6,$8) RETURNING id`, publicID, input.End, input.Username, role, status, placeholder, last4(input.Token), maxPriority+1).Scan(&id); err != nil {
		return err
	}
	tokenEnc, err := s.Cipher.Encrypt(ctx, []byte(input.Token), fmt.Sprintf("bot_token:%d", id))
	if err != nil {
		return err
	}
	secretEnc, err := s.Cipher.Encrypt(ctx, []byte(webhook), fmt.Sprintf("bot_webhook_secret:%d", id))
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE iam.bot SET token_enc=$1,webhook_secret_enc=$2 WHERE id=$3`, tokenEnc, secretEnc, id); err != nil {
		return err
	}
	detail, _ := json.Marshal(struct{ End, Username string }{input.End, input.Username})
	if _, err := s.Audit.AppendTx(ctx, tx, platformaudit.Entry{ActorID: actorID, ActorRole: "system_bootstrap", Action: platformaudit.ActionIntegrationChanged, Target: "bot:" + publicID, Detail: detail, RequestID: "admininit:bot:" + publicID}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func last4(value string) string {
	if len(value) <= 4 {
		return value
	}
	return value[len(value)-4:]
}
