package ids

import (
	"regexp"
	"testing"
)

func TestNewULID(t *testing.T) {
	seen := map[string]struct{}{}
	pattern := regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)
	for range 1000 {
		id, err := NewULID()
		if err != nil {
			t.Fatal(err)
		}
		if !pattern.MatchString(id) {
			t.Fatalf("invalid ULID %q", id)
		}
		if _, ok := seen[id]; ok {
			t.Fatalf("duplicate ULID %q", id)
		}
		seen[id] = struct{}{}
	}
}
