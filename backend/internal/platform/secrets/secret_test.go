package secrets

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/leishen8806/Dr.life-/backend/internal/platform/logx"
)

func TestSecretRedactsAllRepresentations(t *testing.T) {
	plaintext := "highly-sensitive-value"
	secret := NewSecret([]byte(plaintext))
	defer secret.Destroy()
	jsonValue, err := json.Marshal(secret)
	if err != nil {
		t.Fatal(err)
	}
	textValue, err := secret.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	logger := logx.NewLogger(slog.LevelInfo, &logs)
	logger.Info("secret", slog.Any("value", secret))
	outputs := []string{fmt.Sprint(secret), fmt.Sprintf("%+v", secret), fmt.Sprintf("%#v", secret), string(textValue), string(jsonValue), logs.String()}
	for _, output := range outputs {
		if strings.Contains(output, plaintext) || !strings.Contains(output, redacted) {
			t.Fatalf("unsafe output: %s", output)
		}
	}
	revealed := secret.Reveal()
	if string(revealed) != plaintext {
		t.Fatal("Reveal returned wrong value")
	}
}

func TestDisplayHint(t *testing.T) {
	if got := DisplayHint([]byte("1234567")); got != "••••" {
		t.Fatalf("short hint = %q", got)
	}
	if got := DisplayHint([]byte("abcdefgh1234")); got != "••••1234" {
		t.Fatalf("hint = %q", got)
	}
}
