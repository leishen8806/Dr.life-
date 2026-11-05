package main

import (
	"strings"
	"testing"
)

func TestCatalogLoginGrantsAreNarrow(t *testing.T) {
	grants := extraGrants["app_catalog"]
	if len(grants) != 3 {
		t.Fatalf("app_catalog grants = %d, want 3", len(grants))
	}
	joined := strings.Join(grants, "\n")
	for _, want := range []string{
		"GRANT USAGE ON SCHEMA iam TO app_catalog",
		"GRANT SELECT (id, status) ON iam.merchant TO app_catalog",
		"GRANT SELECT (public_id, status, merchant_id) ON iam.account TO app_catalog",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, banned := range []string{"ALL TABLES", "GRANT SELECT ON"} {
		if strings.Contains(joined, banned) {
			t.Errorf("catalog login must not get table-wide read: %q", banned)
		}
	}
}

func TestIAMLoginConsentGrantsAreNarrow(t *testing.T) {
	grants := extraGrants["app_iam"]
	if len(grants) != 5 {
		t.Fatalf("app_iam grants = %d, want 5", len(grants))
	}
	joined := strings.Join(grants, "\n")
	for _, want := range []string{
		"GRANT USAGE ON SCHEMA ops TO app_iam",
		"GRANT SELECT ON ops.legal_doc TO app_iam",
		"GRANT SELECT, INSERT ON ops.consent TO app_iam",
		"GRANT UPDATE (revoked_at) ON ops.consent TO app_iam",
		"GRANT USAGE, SELECT ON SEQUENCE ops.consent_id_seq TO app_iam",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, banned := range []string{"GRANT ALL ON", "GRANT UPDATE ON ops.consent", "GRANT DELETE ON ops.consent"} {
		if strings.Contains(joined, banned) {
			t.Errorf("app_iam consent grant is too broad: %q", banned)
		}
	}
}

func TestFileWorkerLoginUsesOnlyThePromotionRole(t *testing.T) {
	var found *role
	for i := range roles {
		if roles[i].login == "app_file_worker" {
			found = &roles[i]
			break
		}
	}
	if found == nil {
		t.Fatal("app_file_worker login is not provisioned")
	}
	if found.permission != "ops_file_worker" || found.passwordEnv != "FILE_WORKER_DB_PASSWORD" {
		t.Fatalf("file worker role = %+v, want ops_file_worker / FILE_WORKER_DB_PASSWORD", *found)
	}
}
