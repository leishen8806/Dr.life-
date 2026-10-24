package storage

import (
	"crypto/rand"
	"encoding/hex"
)

func randomKey() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic("crypto/rand unavailable")
	}
	return "objects/" + hex.EncodeToString(value[:])
}
