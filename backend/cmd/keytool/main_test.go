package main

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	platformsecrets "github.com/leishen8806/Dr.life-/backend/internal/platform/secrets"
)

func TestGenerateSplitCombine(t *testing.T) {
	var generated, messages bytes.Buffer
	if err := run([]string{"generate"}, strings.NewReader(""), &generated, &messages); err != nil {
		t.Fatal(err)
	}
	keyText := strings.TrimSpace(generated.String())
	key, err := base64.StdEncoding.DecodeString(keyText)
	if err != nil || len(key) != platformsecrets.KeySize {
		t.Fatalf("generated key: %v", err)
	}

	var split bytes.Buffer
	if err := run([]string{"split"}, strings.NewReader(keyText+"\n"), &split, &messages); err != nil {
		t.Fatal(err)
	}
	lines := strings.Fields(split.String())
	if len(lines) != 2 {
		t.Fatalf("split lines = %d", len(lines))
	}
	var combined bytes.Buffer
	if err := run([]string{"combine"}, strings.NewReader(lines[0]+"\n"+lines[1]+"\n"), &combined, &messages); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(combined.String()) != keyText {
		t.Fatal("combined key differs")
	}
}

func TestRejectsSecretsInCommandArguments(t *testing.T) {
	var output bytes.Buffer
	if err := run([]string{"split", "secret-on-command-line"}, strings.NewReader(""), &output, &output); err == nil {
		t.Fatal("secret command argument was accepted")
	}
}
