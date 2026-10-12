package secrets

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"testing"
)

func TestLoadFromEnvironment(t *testing.T) {
	key1, key2 := randomKey(t), randomKey(t)
	t.Setenv("MASTER_KEYS", "old:"+base64.StdEncoding.EncodeToString(key1)+",active:"+base64.StdEncoding.EncodeToString(key2))
	t.Setenv("ACTIVE_MASTER_KEY_ID", "active")
	ring, err := LoadFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	id, got, err := ring.ActiveKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer zero(got)
	if id != "active" || !bytes.Equal(got, key2) {
		t.Fatal("wrong active key")
	}
}

func TestInvalidEnvironmentDoesNotExposeInput(t *testing.T) {
	bad := "do-not-print-this"
	t.Setenv("MASTER_KEYS", "active:"+bad)
	t.Setenv("ACTIVE_MASTER_KEY_ID", "active")
	_, err := LoadFromEnvironment()
	if !errors.Is(err, ErrInvalidKeyConfiguration) {
		t.Fatalf("error = %v", err)
	}
	if bytes.Contains([]byte(err.Error()), []byte(bad)) {
		t.Fatal("error leaked input")
	}
}

func TestSplitCombineAndShareEnvironment(t *testing.T) {
	key := randomKey(t)
	a1, b1, err := SplitKey(key)
	if err != nil {
		t.Fatal(err)
	}
	a2, _, err := SplitKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a1, a2) || bytes.Equal(a1, key) || bytes.Equal(b1, key) {
		t.Fatal("shares are not independently random")
	}
	combined, err := CombineShares(a1, b1)
	if err != nil || !bytes.Equal(combined, key) {
		t.Fatalf("combine failed: %v", err)
	}
	if _, err := CombineShares(a1, b1[:len(b1)-1]); !errors.Is(err, ErrInvalidKeyConfiguration) {
		t.Fatalf("length error = %v", err)
	}
	t.Setenv("MASTER_KEY_SHARE_A", base64.StdEncoding.EncodeToString(a1))
	t.Setenv("MASTER_KEY_SHARE_B", base64.StdEncoding.EncodeToString(b1))
	t.Setenv("ACTIVE_MASTER_KEY_ID", "split-key")
	ring, err := LoadSharesFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	_, loaded, err := ring.ActiveKey(context.Background())
	if err != nil || !bytes.Equal(loaded, key) {
		t.Fatalf("loaded split key: %v", err)
	}
}
