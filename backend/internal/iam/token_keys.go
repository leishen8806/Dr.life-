package iam

import (
	"encoding/base64"
	"os"
	"strings"

	"github.com/leishen8806/Dr.life-/backend/internal/platform/secrets"
)

func LoadJWTKeyringFromEnvironment() (*secrets.Keyring, error) {
	keys := make(map[string][]byte)
	defer func() {
		for _, key := range keys {
			wipe(key)
		}
	}()
	for _, entry := range strings.Split(os.Getenv("JWT_SIGNING_KEYS"), ",") {
		id, encoded, ok := strings.Cut(strings.TrimSpace(entry), ":")
		if !ok {
			return nil, secrets.ErrInvalidKeyConfiguration
		}
		key, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, secrets.ErrInvalidKeyConfiguration
		}
		keys[id] = key
	}
	return secrets.NewKeyring(keys, os.Getenv("JWT_ACTIVE_KEY_ID"))
}
