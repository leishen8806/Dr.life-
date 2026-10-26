// Package iam contains platform identity and authorization rules.
package iam

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/argon2"
)

type Role string

const (
	RoleSuper      Role = "super"
	RoleOperations Role = "ops"
	RoleFinance    Role = "finance"
	RoleLogistics  Role = "logi"
	RoleSupport    Role = "cs"
	RoleAuditor    Role = "auditor"
)

type Module string

const (
	ModuleDashboard   Module = "dash"
	ModuleUsers       Module = "usr"
	ModuleMerchants   Module = "mer"
	ModuleProducts    Module = "prod"
	ModuleOrders      Module = "ord"
	ModuleFund        Module = "fund"
	ModuleWithdrawals Module = "wd"
	ModuleDisputes    Module = "mod"
	ModuleLogistics   Module = "logi"
	ModuleIntegration Module = "integ"
	ModuleRBAC        Module = "rbac"
	ModuleSensitive   Module = "sens"
	ModuleConfig      Module = "cfg"
	ModuleFinance     Module = "fin"
	ModuleAudit       Module = "aud"
	ModuleOperations  Module = "ops"
	ModuleDispatch    Module = "disp"
)

type Permission string

const (
	PermissionNone Permission = "none"
	PermissionView Permission = "view"
	PermissionEdit Permission = "edit"
)

var (
	adminRoles   = []Role{RoleSuper, RoleOperations, RoleFinance, RoleLogistics, RoleSupport, RoleAuditor}
	adminModules = []Module{
		ModuleDashboard, ModuleUsers, ModuleMerchants, ModuleProducts, ModuleOrders,
		ModuleFund, ModuleWithdrawals, ModuleDisputes, ModuleLogistics, ModuleIntegration,
		ModuleRBAC, ModuleSensitive, ModuleConfig, ModuleFinance, ModuleAudit,
		ModuleOperations, ModuleDispatch,
	}
)

// AdminMatrix is a complete role x module permission matrix. It is intentionally
// value-like so callers can run self-checks on a proposed matrix before persisting it.
type AdminMatrix map[Role]map[Module]Permission

func DefaultAdminMatrix() AdminMatrix {
	rows := []string{
		"evvvvv", "eevnvv", "eevvvv", "eennvv", "evvvvv", "enennv", "enennv",
		"eennnv", "evnenv", "ennvnv", "ennnnv", "envnvn", "eennnv", "enennv",
		"envnne", "eevvvv", "eevnnv",
	}
	matrix := make(AdminMatrix, len(adminRoles))
	for _, role := range adminRoles {
		matrix[role] = make(map[Module]Permission, len(adminModules))
	}
	for i, module := range adminModules {
		for j, role := range adminRoles {
			matrix[role][module] = decodePermission(rows[i][j])
		}
	}
	return matrix
}

func (m AdminMatrix) Level(role Role, module Module) Permission {
	if m[role] == nil {
		return PermissionNone
	}
	return m[role][module]
}

func (m AdminMatrix) Set(role Role, module Module, level Permission) {
	if m[role] == nil {
		m[role] = make(map[Module]Permission)
	}
	m[role][module] = level
}

func (m AdminMatrix) Can(role Role, module Module, required Permission) bool {
	return permissionRank(m.Level(role, module)) >= permissionRank(required)
}

func (m AdminMatrix) Entries() int {
	count := 0
	for _, permissions := range m {
		count += len(permissions)
	}
	return count
}

func decodePermission(value byte) Permission {
	switch value {
	case 'e':
		return PermissionEdit
	case 'v':
		return PermissionView
	default:
		return PermissionNone
	}
}

func permissionRank(value Permission) int {
	switch value {
	case PermissionView:
		return 1
	case PermissionEdit:
		return 2
	default:
		return 0
	}
}

type ConflictCode string

const (
	ConflictDisputeAndWithdrawal ConflictCode = "dispute_and_withdrawal"
	ConflictRBACSelfEscalation   ConflictCode = "rbac_self_escalation"
	ConflictAuditorIndependence  ConflictCode = "auditor_independence"
	WarningSensitiveEditing      ConflictCode = "sensitive_editing"
	WarningMerchantWithdrawal    ConflictCode = "merchant_and_withdrawal"
	WarningIntegrationWithdrawal ConflictCode = "integration_and_withdrawal"
)

type SeparationConflict struct {
	Code     ConflictCode
	Role     Role
	Severity string
}

// CheckSeparationOfDuties implements the six checks in the RBAC policy.
func CheckSeparationOfDuties(matrix AdminMatrix) []SeparationConflict {
	var findings []SeparationConflict
	for _, role := range adminRoles {
		// The policy fixes the super-admin matrix to full edit access. Separation
		// checks are for delegable roles and must not flag that fixed exception.
		if role == RoleSuper {
			continue
		}
		canEdit := func(module Module) bool { return matrix.Can(role, module, PermissionEdit) }
		if canEdit(ModuleDisputes) && canEdit(ModuleWithdrawals) {
			findings = append(findings, SeparationConflict{ConflictDisputeAndWithdrawal, role, "conflict"})
		}
		if canEdit(ModuleRBAC) {
			findings = append(findings, SeparationConflict{ConflictRBACSelfEscalation, role, "conflict"})
		}
		if role == RoleAuditor && (canEdit(ModuleFund) || canEdit(ModuleWithdrawals) || canEdit(ModuleRBAC) || canEdit(ModuleConfig) || canEdit(ModuleIntegration)) {
			findings = append(findings, SeparationConflict{ConflictAuditorIndependence, role, "conflict"})
		}
		if canEdit(ModuleSensitive) {
			findings = append(findings, SeparationConflict{WarningSensitiveEditing, role, "warning"})
		}
		if canEdit(ModuleMerchants) && canEdit(ModuleWithdrawals) {
			findings = append(findings, SeparationConflict{WarningMerchantWithdrawal, role, "warning"})
		}
		if canEdit(ModuleIntegration) && canEdit(ModuleWithdrawals) {
			findings = append(findings, SeparationConflict{WarningIntegrationWithdrawal, role, "warning"})
		}
	}
	return findings
}

func MaskPhone(phone string, sensitive Permission) string {
	if permissionRank(sensitive) < permissionRank(PermissionView) {
		return "•••"
	}
	return phone
}

const (
	argonMemory  uint32 = 64 * 1024
	argonTime    uint32 = 3
	argonThreads uint8  = 4
	argonKeyLen  uint32 = 32
)

var ErrInvalidPasswordHash = errors.New("iam: invalid password hash")

var (
	ErrPermissionDenied = errors.New("iam: permission denied")
	// ErrOutOfScope is deliberately distinct from a role failure. API handlers
	// map it to a not-found response to avoid exposing another region's data.
	ErrOutOfScope = errors.New("iam: resource outside data scope")
)

type AdminIdentity struct {
	Role      Role
	DataScope []string
}

// AuthorizeAdmin checks module permission before the target's region. Callers
// must pass the target's actual region from their scoped query, never a client
// supplied value. An empty scope is the documented all-regions default.
func AuthorizeAdmin(matrix AdminMatrix, identity AdminIdentity, module Module, required Permission, region string) error {
	if !matrix.Can(identity.Role, module, required) {
		return ErrPermissionDenied
	}
	if len(identity.DataScope) == 0 || region == "" {
		return nil
	}
	for _, allowed := range identity.DataScope {
		if allowed == region {
			return nil
		}
	}
	return ErrOutOfScope
}

func HashPassword(password string) (string, error) {
	if len(password) == 0 || len(password) > 1024 {
		return "", errors.New("iam: invalid password")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", errors.New("iam: password salt source failed")
	}
	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	defer zero(hash)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argonMemory, argonTime, argonThreads, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)), nil
}

func VerifyPassword(encoded, password string) bool {
	params, salt, expected, err := parsePasswordHash(encoded)
	if err != nil || len(password) == 0 {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, params.time, params.memory, params.threads, uint32(len(expected)))
	defer zero(actual)
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

type argonParams struct {
	memory  uint32
	time    uint32
	threads uint8
}

func parsePasswordHash(encoded string) (argonParams, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return argonParams{}, nil, nil, ErrInvalidPasswordHash
	}
	var params argonParams
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &params.memory, &params.time, &params.threads); err != nil || params.memory < 8*1024 || params.time == 0 || params.threads == 0 || params.threads > 32 {
		return argonParams{}, nil, nil, ErrInvalidPasswordHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 16 {
		return argonParams{}, nil, nil, ErrInvalidPasswordHash
	}
	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(hash) < 16 || len(hash) > 128 {
		return argonParams{}, nil, nil, ErrInvalidPasswordHash
	}
	return params, salt, hash, nil
}

func ValidateTOTP(secret, code string, at time.Time) bool {
	if len(code) != 6 && len(code) != 8 {
		return false
	}
	digits := otp.DigitsSix
	if len(code) == 8 {
		digits = otp.DigitsEight
	}
	ok, err := totp.ValidateCustom(code, secret, at.UTC(), totp.ValidateOpts{Period: 30, Skew: 0, Digits: digits, Algorithm: otp.AlgorithmSHA1})
	return err == nil && ok
}

type TOTPSecretCipher interface {
	Encrypt(context.Context, []byte, string) ([]byte, error)
	Decrypt(context.Context, []byte, string) ([]byte, error)
}

type AdminStatus string

const AdminStatusActive AdminStatus = "active"

var ErrAdminAuthentication = errors.New("iam: administrator authentication failed")

// AdminCredentials is the minimal credential record read from iam.admin_user.
// TOTPSecretEnc must only be the envelope-encrypted database value.
type AdminCredentials struct {
	PublicID      string
	PasswordHash  string
	TOTPSecretEnc []byte
	Status        AdminStatus
}

// EncryptTOTPSecret encrypts a TOTP secret with an AAD that binds it to one
// administrator. This convention is approved as admin_totp:<public_id>.
func EncryptTOTPSecret(ctx context.Context, cipher TOTPSecretCipher, publicID, secret string) ([]byte, error) {
	if cipher == nil || publicID == "" || secret == "" {
		return nil, ErrAdminAuthentication
	}
	return cipher.Encrypt(ctx, []byte(secret), adminTOTPAAD(publicID))
}

// AuthenticateAdmin performs password and TOTP checks without disclosing which
// credential failed. The caller is responsible for fetching only an active
// administrator record and recording a successful login transactionally.
func AuthenticateAdmin(ctx context.Context, cipher TOTPSecretCipher, admin AdminCredentials, password, code string, at time.Time) error {
	if cipher == nil || admin.Status != AdminStatusActive || admin.PublicID == "" || len(admin.TOTPSecretEnc) == 0 || !VerifyPassword(admin.PasswordHash, password) {
		return ErrAdminAuthentication
	}
	secret, err := cipher.Decrypt(ctx, admin.TOTPSecretEnc, adminTOTPAAD(admin.PublicID))
	if err != nil {
		return ErrAdminAuthentication
	}
	defer zero(secret)
	if !ValidateTOTP(string(secret), code, at) {
		return ErrAdminAuthentication
	}
	return nil
}

func adminTOTPAAD(publicID string) string { return "admin_totp:" + publicID }

func zero(value []byte) {
	for i := range value {
		value[i] = 0
	}
}
