package secrets

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"strings"
)

const KeySize = 32

var (
	ErrInvalidKeyConfiguration = errors.New("secrets: invalid master key configuration")
	ErrKeyNotFound             = errors.New("secrets: master key not found")
)

type KeyProvider interface {
	ActiveKey(context.Context) (string, []byte, error)
	KeyByID(context.Context, string) ([]byte, error)
}

type Keyring struct {
	active string
	keys   map[string][KeySize]byte
}

func NewKeyring(keys map[string][]byte, active string) (*Keyring, error) {
	if !validKeyID(active) || len(keys) == 0 {
		return nil, ErrInvalidKeyConfiguration
	}
	ring := &Keyring{active: active, keys: make(map[string][KeySize]byte, len(keys))}
	for id, key := range keys {
		if !validKeyID(id) || len(key) != KeySize {
			return nil, ErrInvalidKeyConfiguration
		}
		var stored [KeySize]byte
		copy(stored[:], key)
		ring.keys[id] = stored
	}
	if _, ok := ring.keys[active]; !ok {
		return nil, ErrInvalidKeyConfiguration
	}
	return ring, nil
}

func LoadFromEnvironment() (*Keyring, error) {
	entries := strings.Split(os.Getenv("MASTER_KEYS"), ",")
	keys := make(map[string][]byte, len(entries))
	defer func() {
		for _, key := range keys {
			zero(key)
		}
	}()
	for _, entry := range entries {
		id, encoded, ok := strings.Cut(strings.TrimSpace(entry), ":")
		if !ok || encoded == "" {
			return nil, ErrInvalidKeyConfiguration
		}
		key, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(key) != KeySize {
			zero(key)
			return nil, ErrInvalidKeyConfiguration
		}
		if _, exists := keys[id]; exists {
			zero(key)
			return nil, ErrInvalidKeyConfiguration
		}
		keys[id] = key
	}
	return NewKeyring(keys, os.Getenv("ACTIVE_MASTER_KEY_ID"))
}

func LoadSharesFromEnvironment() (*Keyring, error) {
	shareA, errA := base64.StdEncoding.DecodeString(os.Getenv("MASTER_KEY_SHARE_A"))
	shareB, errB := base64.StdEncoding.DecodeString(os.Getenv("MASTER_KEY_SHARE_B"))
	if errA != nil || errB != nil {
		zero(shareA)
		zero(shareB)
		return nil, ErrInvalidKeyConfiguration
	}
	defer zero(shareA)
	defer zero(shareB)
	key, err := CombineShares(shareA, shareB)
	if err != nil {
		return nil, err
	}
	defer zero(key)
	active := os.Getenv("ACTIVE_MASTER_KEY_ID")
	return NewKeyring(map[string][]byte{active: key}, active)
}

func (k *Keyring) ActiveKey(ctx context.Context) (string, []byte, error) {
	key, err := k.key(ctx, k.active)
	return k.active, key, err
}

func (k *Keyring) KeyByID(ctx context.Context, id string) ([]byte, error) {
	return k.key(ctx, id)
}

func (k *Keyring) key(ctx context.Context, id string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stored, ok := k.keys[id]
	if !ok {
		return nil, ErrKeyNotFound
	}
	key := make([]byte, KeySize)
	copy(key, stored[:])
	return key, nil
}

func SplitKey(key []byte) ([]byte, []byte, error) {
	if len(key) != KeySize {
		return nil, nil, ErrInvalidKeyConfiguration
	}
	shareA := make([]byte, KeySize)
	if _, err := rand.Read(shareA); err != nil {
		return nil, nil, errors.New("secrets: random source failed")
	}
	shareB := make([]byte, KeySize)
	for i := range key {
		shareB[i] = key[i] ^ shareA[i]
	}
	return shareA, shareB, nil
}

func CombineShares(shareA, shareB []byte) ([]byte, error) {
	if len(shareA) != KeySize || len(shareB) != KeySize {
		return nil, ErrInvalidKeyConfiguration
	}
	key := make([]byte, KeySize)
	for i := range shareA {
		key[i] = shareA[i] ^ shareB[i]
	}
	return key, nil
}

func validKeyID(id string) bool {
	if len(id) < 1 || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' && r != '_' && r != '.' {
			return false
		}
	}
	return true
}

func zero(value []byte) {
	for i := range value {
		value[i] = 0
	}
}
