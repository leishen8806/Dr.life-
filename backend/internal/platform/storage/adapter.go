package storage

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

func randomKey() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic("crypto/rand unavailable")
	}
	return "objects/" + hex.EncodeToString(value[:])
}

func validRandomKey(key string) bool {
	const prefix = "objects/"
	if !strings.HasPrefix(key, prefix) || len(key) != len(prefix)+32 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(key, prefix))
	return err == nil
}
