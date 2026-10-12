package secrets

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSOPSCheckAcceptsEncryptedAndRejectsPlaintext(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires POSIX shell; covered in Linux CI")
	}
	_, current, _, _ := runtime.Caller(0)
	script := filepath.Join(filepath.Dir(current), "..", "..", "..", "scripts", "check-sops-encrypted.sh")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "encrypted.yaml"), []byte("value: ENC[AES256_GCM,data:x]\nsops:\n  version: 3.9.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", script)
	cmd.Env = append(os.Environ(), "SECRETS_DIR="+dir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("encrypted file rejected: %v: %s", err, output)
	}
	if err := os.WriteFile(filepath.Join(dir, "plaintext.yaml"), []byte("value: plaintext\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("sh", script)
	cmd.Env = append(os.Environ(), "SECRETS_DIR="+dir)
	if err := cmd.Run(); err == nil {
		t.Fatal("plaintext file accepted")
	}
}
