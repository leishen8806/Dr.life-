package secrets

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
)

func TestEncryptDecryptUsesFreshKeysAndNonces(t *testing.T) {
	envelope := testEnvelope(t, map[string][]byte{"key-1": randomKey(t)}, "key-1")
	plaintext := []byte("telegram-test-token")
	aad := "bot_token:123"
	one, err := envelope.Encrypt(context.Background(), plaintext, aad)
	if err != nil {
		t.Fatal(err)
	}
	two, err := envelope.Encrypt(context.Background(), plaintext, aad)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(one, two) {
		t.Fatal("two encryptions produced the same blob")
	}
	got, err := envelope.Decrypt(context.Background(), one, aad)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("got %q", got)
	}
}

func TestTamperingAndAADMismatchFail(t *testing.T) {
	envelope := testEnvelope(t, map[string][]byte{"key-1": randomKey(t)}, "key-1")
	blob, err := envelope.Encrypt(context.Background(), []byte("secret-value"), "integration_key:7")
	if err != nil {
		t.Fatal(err)
	}
	for i := range blob {
		tampered := append([]byte(nil), blob...)
		tampered[i] ^= 1
		if _, err := envelope.Decrypt(context.Background(), tampered, "integration_key:7"); err == nil {
			t.Fatalf("tampering byte %d was accepted", i)
		}
	}
	if _, err := envelope.Decrypt(context.Background(), blob, "integration_key:8"); err == nil {
		t.Fatal("different AAD was accepted")
	}
}

func TestWrongKeyFailsWithoutLeakingValues(t *testing.T) {
	plaintext := []byte("never-print-this")
	right := testEnvelope(t, map[string][]byte{"key-1": randomKey(t)}, "key-1")
	blob, err := right.Encrypt(context.Background(), plaintext, "bot_token:1")
	if err != nil {
		t.Fatal(err)
	}
	wrong := testEnvelope(t, map[string][]byte{"key-1": randomKey(t)}, "key-1")
	_, err = wrong.Decrypt(context.Background(), blob, "bot_token:1")
	if err == nil {
		t.Fatal("wrong key was accepted")
	}
	if strings.Contains(err.Error(), string(plaintext)) || strings.Contains(err.Error(), "bot_token:1") {
		t.Fatalf("error leaked protected input: %v", err)
	}
}

func TestRewrapAllowsOldKeyRemoval(t *testing.T) {
	oldKey, newKey := randomKey(t), randomKey(t)
	oldEnvelope := testEnvelope(t, map[string][]byte{"old": oldKey}, "old")
	blob, err := oldEnvelope.Encrypt(context.Background(), []byte("value"), "bot_token:9")
	if err != nil {
		t.Fatal(err)
	}
	rotating := testEnvelope(t, map[string][]byte{"old": oldKey, "new": newKey}, "new")
	if _, err := rotating.Decrypt(context.Background(), blob, "bot_token:9"); err != nil {
		t.Fatal(err)
	}
	rewrapped, err := rotating.Rewrap(context.Background(), blob, "bot_token:9")
	if err != nil {
		t.Fatal(err)
	}
	before, err := parsePayload(blob)
	if err != nil {
		t.Fatal(err)
	}
	after, err := parsePayload(rewrapped)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before.dataNonce, after.dataNonce) || !bytes.Equal(before.dataCiphertext, after.dataCiphertext) {
		t.Fatal("rewrap changed data ciphertext")
	}
	newOnly := testEnvelope(t, map[string][]byte{"new": newKey}, "new")
	if got, err := newOnly.Decrypt(context.Background(), rewrapped, "bot_token:9"); err != nil || string(got) != "value" {
		t.Fatalf("rewrapped decrypt: %q, %v", got, err)
	}
	if _, err := newOnly.Decrypt(context.Background(), blob, "bot_token:9"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("unwrapped old data error = %v", err)
	}
}

func testEnvelope(t *testing.T, keys map[string][]byte, active string) *Envelope {
	t.Helper()
	ring, err := NewKeyring(keys, active)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := NewEnvelope(ring)
	if err != nil {
		t.Fatal(err)
	}
	return envelope
}

func randomKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	return key
}
