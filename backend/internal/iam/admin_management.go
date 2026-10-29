package iam

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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

// AdminManagementService owns sensitive administrator creation. The initial
// password and provisioning URI are returned only to the direct caller and are
// deliberately not persisted in result-like database columns or audit detail.
type AdminManagementService struct {
	DB     *pgxpool.Pool
	Cipher TOTPSecretCipher
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
