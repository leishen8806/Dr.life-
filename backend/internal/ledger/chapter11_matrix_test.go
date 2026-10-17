package ledger_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var chapter11RequiredIDs = []string{"C11-01", "C11-02", "C11-03", "C11-04", "C11-05", "C11-06", "C11-07", "C11-08", "C11-09", "C11-10a", "C11-10b"}
var matrixIDPattern = regexp.MustCompile(`(?m)^\| (C11-[^ ]+) \|`)

type chapter11Case struct {
	id      string
	tests   []string
	blocked string
}

var chapter11Cases = []chapter11Case{
	{id: "C11-01", tests: []string{"TestBusinessPostingIntegration", "TestBusinessRandomSequenceMaintainsInvariant"}},
	{id: "C11-02", tests: []string{"TestPostIntegration", "TestHoldIntegration", "TestWithdrawalConcurrencyIntegration", "TestReverseConcurrencyIntegration", "TestAdjustIntegration"}},
	{id: "C11-03", tests: []string{"TestPostIntegration", "TestHoldIntegration", "TestWithdrawalConcurrencyIntegration", "TestReverseConcurrencyIntegration"}},
	{id: "C11-04", tests: []string{"TestAllocateBoundaries", "TestAllocateProperty", "TestAllocateRejectsInvalidShares"}},
	{id: "C11-05", tests: []string{"TestLedgerMigrationAndConstraints", "TestPostIntegration", "TestHoldIntegration", "TestSnapshotDailyImmutableAndDetectsDrift", "TestDatabaseFramework"}},
	{id: "C11-06", tests: []string{"TestReverseIntegration", "TestReverseConcurrencyIntegration", "TestReverseRandomSequenceIntegration"}},
	{id: "C11-07", tests: []string{"TestWithdrawalWorkflowIntegration", "TestWithdrawalConcurrencyIntegration"}},
	{id: "C11-08", blocked: "L-08"},
	{id: "C11-09", tests: []string{"TestLedgerMigrationAndConstraints", "TestPostIntegration"}},
	{id: "C11-10a", tests: []string{"TestChapter11BackupRestoreIntegration"}},
	{id: "C11-10b", blocked: "Q-03"},
}

func TestChapter11Matrix(t *testing.T) {
	source := readTestSources(t, filepath.Join("..", ".."))
	matrix, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "TESTMATRIX.md"))
	if err != nil {
		t.Fatal(err)
	}
	expected := makeIDSet(chapter11RequiredIDs)
	seen := make(map[string]bool, len(chapter11Cases))
	for _, item := range chapter11Cases {
		if seen[item.id] {
			t.Fatalf("duplicate matrix id %q", item.id)
		}
		seen[item.id] = true
		if !strings.Contains(string(matrix), "| "+item.id+" |") {
			t.Fatalf("%s missing from TESTMATRIX.md", item.id)
		}
		if len(item.tests) == 0 && item.blocked == "" {
			t.Fatalf("%s has neither tests nor a blocker", item.id)
		}
		if item.blocked != "" && !regexp.MustCompile(`^[A-Z]+-[0-9]+$`).MatchString(item.blocked) {
			t.Fatalf("%s has invalid blocker %q", item.id, item.blocked)
		}
		if item.blocked != "" && !strings.Contains(string(matrix), "BLOCKED("+item.blocked+")") {
			t.Fatalf("%s blocker missing from TESTMATRIX.md", item.id)
		}
		for _, name := range item.tests {
			if !strings.Contains(source, "func "+name+"(") {
				t.Fatalf("%s references missing test %s", item.id, name)
			}
		}
	}
	assertIDSetsEqual(t, "code", seen, expected)
	documented := make(map[string]bool)
	for _, match := range matrixIDPattern.FindAllStringSubmatch(string(matrix), -1) {
		documented[match[1]] = true
	}
	assertIDSetsEqual(t, "TESTMATRIX.md", documented, expected)
}

func makeIDSet(ids []string) map[string]bool {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set
}

func assertIDSetsEqual(t *testing.T, name string, got, want map[string]bool) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s has %d IDs, want %d", name, len(got), len(want))
	}
	for id := range want {
		if !got[id] {
			t.Fatalf("%s missing required ID %s", name, id)
		}
	}
}

func readTestSources(t *testing.T, root string) string {
	t.Helper()
	var source strings.Builder
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		source.Write(body)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return source.String()
}
