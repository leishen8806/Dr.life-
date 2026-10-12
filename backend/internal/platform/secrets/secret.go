package secrets

import (
	"encoding/json"
	"log/slog"
)

const redacted = "[REDACTED]"

type Secret struct {
	value []byte
}

func NewSecret(value []byte) Secret {
	copyValue := append([]byte(nil), value...)
	return Secret{value: copyValue}
}

func (s Secret) Reveal() []byte             { return append([]byte(nil), s.value...) }
func (Secret) String() string               { return redacted }
func (Secret) GoString() string             { return redacted }
func (Secret) MarshalText() ([]byte, error) { return []byte(redacted), nil }
func (Secret) LogValue() slog.Value         { return slog.StringValue(redacted) }
func (Secret) MarshalJSON() ([]byte, error) { return json.Marshal(redacted) }
func (s *Secret) Destroy()                  { zero(s.value); s.value = nil }

func DisplayHint(plaintext []byte) string {
	runes := []rune(string(plaintext))
	if len(runes) < 8 {
		return "••••"
	}
	return "••••" + string(runes[len(runes)-4:])
}
