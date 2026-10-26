package iam

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/leishen8806/Dr.life-/backend/internal/platform/secrets"
)

func TestDefaultAdminMatrixMatchesPolicyAndIsSeparationSafe(t *testing.T) {
	matrix := DefaultAdminMatrix()
	if matrix.Entries() != 102 {
		t.Fatalf("permission entries = %d, want 102", matrix.Entries())
	}
	if got := matrix.Level(RoleFinance, ModuleFund); got != PermissionEdit {
		t.Fatalf("finance/fund = %s, want edit", got)
	}
	if got := matrix.Level(RoleOperations, ModuleFund); got != PermissionNone {
		t.Fatalf("operations/fund = %s, want none", got)
	}
	if got := matrix.Level(RoleSupport, ModuleSensitive); got != PermissionView {
		t.Fatalf("support/sensitive = %s, want view", got)
	}
	if conflicts := CheckSeparationOfDuties(matrix); len(conflicts) != 0 {
		t.Fatalf("default matrix conflicts = %#v, want none", conflicts)
	}
}

func TestSeparationOfDutiesRejectsDisputeAndWithdrawalCombination(t *testing.T) {
	matrix := DefaultAdminMatrix()
	matrix.Set(RoleFinance, ModuleDisputes, PermissionEdit)
	conflicts := CheckSeparationOfDuties(matrix)
	if !containsConflict(conflicts, ConflictDisputeAndWithdrawal) {
		t.Fatalf("conflicts = %#v, want %q", conflicts, ConflictDisputeAndWithdrawal)
	}
}

func TestSeparationOfDutiesWarnsMerchantVerificationWithWithdrawal(t *testing.T) {
	matrix := DefaultAdminMatrix()
	matrix.Set(RoleOperations, ModuleWithdrawals, PermissionEdit)
	if !containsConflict(CheckSeparationOfDuties(matrix), WarningMerchantWithdrawal) {
		t.Fatal("ops with merchant edit and withdrawal edit must raise the merchant/withdrawal warning")
	}

	// Dispute plus withdrawal is a hard conflict, but it must not masquerade as the merchant warning
	// when the role cannot edit merchants.
	matrix = DefaultAdminMatrix()
	matrix.Set(RoleFinance, ModuleDisputes, PermissionEdit)
	if containsConflict(CheckSeparationOfDuties(matrix), WarningMerchantWithdrawal) {
		t.Fatal("finance cannot edit merchants; the merchant/withdrawal warning must not fire")
	}
}

func TestMaskPhoneRequiresSensitivePermission(t *testing.T) {
	if got := MaskPhone("+85512345678", PermissionNone); got != "•••" {
		t.Fatalf("masked phone = %q, want bullets", got)
	}
	if got := MaskPhone("+85512345678", PermissionView); got != "+85512345678" {
		t.Fatalf("visible phone = %q", got)
	}
}

func TestAuthorizeAdminEnforcesPermissionAndRegionalScope(t *testing.T) {
	matrix := DefaultAdminMatrix()
	identity := AdminIdentity{Role: RoleOperations, DataScope: []string{"phnom-penh"}}
	if err := AuthorizeAdmin(matrix, identity, ModuleMerchants, PermissionView, "phnom-penh"); err != nil {
		t.Fatalf("in-scope merchant view: %v", err)
	}
	if err := AuthorizeAdmin(matrix, identity, ModuleFund, PermissionView, "phnom-penh"); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("fund view error = %v, want permission denied", err)
	}
	if err := AuthorizeAdmin(matrix, identity, ModuleMerchants, PermissionView, "siem-reap"); !errors.Is(err, ErrOutOfScope) {
		t.Fatalf("cross-region error = %v, want out of scope", err)
	}
	if err := AuthorizeAdmin(matrix, AdminIdentity{Role: RoleFinance}, ModuleFund, PermissionView, "siem-reap"); err != nil {
		t.Fatalf("empty scope should mean all regions: %v", err)
	}
}

func TestAdministratorTOTPSecretIsBoundToPublicID(t *testing.T) {
	ctx := context.Background()
	ring, err := secrets.NewKeyring(map[string][]byte{"test-key": bytesOf(7, secrets.KeySize)}, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := secrets.NewEnvelope(ring)
	if err != nil {
		t.Fatal(err)
	}

	passwordHash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	const publicID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	secret, err := EncryptTOTPSecret(ctx, cipher, publicID, "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ")
	if err != nil {
		t.Fatal(err)
	}
	admin := AdminCredentials{PublicID: publicID, PasswordHash: passwordHash, TOTPSecretEnc: secret, Status: AdminStatusActive}
	if err := AuthenticateAdmin(ctx, cipher, admin, "correct horse battery staple", "94287082", time.Unix(59, 0)); err != nil {
		t.Fatalf("authenticate valid administrator: %v", err)
	}

	admin.PublicID = "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	if err := AuthenticateAdmin(ctx, cipher, admin, "correct horse battery staple", "94287082", time.Unix(59, 0)); !errors.Is(err, ErrAdminAuthentication) {
		t.Fatalf("AAD-mismatched administrator error = %v, want authentication failure", err)
	}
}

func TestPasswordHashAndTOTPValidation(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("hash = %q, want argon2id encoded hash", hash)
	}
	if !VerifyPassword(hash, "correct horse battery staple") || VerifyPassword(hash, "wrong") {
		t.Fatal("password verification did not distinguish correct and incorrect password")
	}

	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	if !ValidateTOTP(secret, "94287082", time.Unix(59, 0)) {
		t.Fatal("RFC 6238 test vector must validate")
	}
	if ValidateTOTP(secret, "94287081", time.Unix(59, 0)) {
		t.Fatal("invalid TOTP code must be rejected")
	}
}

func containsConflict(conflicts []SeparationConflict, want ConflictCode) bool {
	for _, conflict := range conflicts {
		if conflict.Code == want {
			return true
		}
	}
	return false
}

func bytesOf(value byte, count int) []byte {
	result := make([]byte, count)
	for i := range result {
		result[i] = value
	}
	return result
}
