package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileAnchorSinkAppendsJSONLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anchors.jsonl")
	sink := FileAnchorSink{Path: path}
	for _, day := range []string{"2026-10-25", "2026-10-26"} {
		if err := sink.Put(context.Background(), AnchorRecord{Day: day, LastID: 7, LastHash: "abc"}); err != nil {
			t.Fatal(err)
		}
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(content, []byte("\n")) != 2 || !bytes.Contains(content, []byte(`"day":"2026-10-25"`)) {
		t.Fatalf("anchor file = %s", content)
	}
}

func TestCanonicalJSONAndRecursiveRedaction(t *testing.T) {
	got, err := sanitizeDetail(json.RawMessage(`{"z":1.00,"before":{"phone_number":"0123456789","name":"visible"},"a":1e0,"tokens":[{"secretKey":"hidden"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":1,"before":{"name":"visible","phone_number":"[REDACTED]"},"tokens":"[REDACTED]","z":1}`
	if string(got) != want {
		t.Fatalf("canonical detail = %s, want %s", got, want)
	}
}

func TestDetailBoundaryValidation(t *testing.T) {
	if _, err := sanitizeDetail(json.RawMessage(`[]`)); err == nil {
		t.Fatal("array detail accepted")
	}
	if _, err := sanitizeDetail(json.RawMessage(`{"safe":"` + string(bytes.Repeat([]byte("x"), MaxDetailBytes)) + `"}`)); err == nil {
		t.Fatal("oversized detail accepted")
	}
}

func TestHashVector(t *testing.T) {
	entry := Entry{Time: time.Date(2026, 10, 26, 1, 2, 3, 456789000, time.FixedZone("ICT", 7*3600)), ActorID: 42, ActorRole: "admin", Action: ActionPermissionChanged, Target: "account:01ABC", IP: "203.0.113.7", RequestID: "req_1"}
	canonical := []byte(`{"a":1,"b":"x"}`)
	got := hashHex(computeHash(GenesisHash, entry, canonical))
	const want = "d1148cba728dbb4dd361c2f93ea97cda547261c73349e2ba85d8fe501f08c54d"
	if got != want {
		t.Fatalf("hash = %s, want %s", got, want)
	}
}

func TestLengthPrefixAvoidsConcatenationAmbiguity(t *testing.T) {
	timeValue := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	left := Entry{Time: timeValue, ActorID: 1, ActorRole: "ab", Action: ActionLogin, Target: "c", RequestID: "r"}
	right := Entry{Time: timeValue, ActorID: 1, ActorRole: "a", Action: ActionLogin, Target: "bc", RequestID: "r"}
	if bytes.Equal(computeHash(GenesisHash, left, []byte(`{}`)), computeHash(GenesisHash, right, []byte(`{}`))) {
		t.Fatal("ambiguous concatenation produced the same hash")
	}
}

func TestNormalizeNumber(t *testing.T) {
	for _, test := range []struct{ in, want string }{{"1.0", "1"}, {"1e0", "1"}, {"0.00100", "0.001"}, {"1.2e3", "1200"}, {"1.2e-3", "0.0012"}, {"-0.0", "0"}} {
		got, err := normalizeNumber(test.in)
		if err != nil || got != test.want {
			t.Fatalf("normalizeNumber(%q) = %q, %v; want %q", test.in, got, err, test.want)
		}
	}
}
