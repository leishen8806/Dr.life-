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
