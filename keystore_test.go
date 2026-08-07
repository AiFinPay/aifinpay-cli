package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withTestKeystore(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv(envHome, home)
	t.Setenv(envKeystorePwd, "correct horse battery staple")
	t.Setenv(envSecret, "")
	return filepath.Join(home, keystoreFil)
}

func TestEncryptedKeystoreRoundTripContainsNoPlaintextSecret(t *testing.T) {
	path := withTestKeystore(t)
	secret, _, err := newAgentSecret()
	if err != nil {
		t.Fatal(err)
	}
	if err := saveKeystore(secret); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secret) || strings.Contains(string(raw), `"secret"`) {
		t.Fatalf("plaintext secret leaked into keystore: %s", raw)
	}
	var ks encryptedKeystore
	if err := json.Unmarshal(raw, &ks); err != nil {
		t.Fatal(err)
	}
	if ks.Version != keystoreV2 || ks.Cipher != "aes-256-gcm" || ks.KDF != "pbkdf2-hmac-sha256" {
		t.Fatalf("unexpected keystore policy: %+v", ks)
	}
	if got := loadKeystoreSecret(); got != secret {
		t.Fatalf("round trip mismatch")
	}
}

func TestWrongPassphraseDoesNotDecrypt(t *testing.T) {
	_ = withTestKeystore(t)
	secret, _, err := newAgentSecret()
	if err != nil {
		t.Fatal(err)
	}
	if err := saveKeystore(secret); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envKeystorePwd, "this is the wrong passphrase")
	if got := loadKeystoreSecret(); got != "" {
		t.Fatal("wrong passphrase decrypted keystore")
	}
}

func TestKeystorePermissionsAreRepaired(t *testing.T) {
	path := withTestKeystore(t)
	secret, _, err := newAgentSecret()
	if err != nil {
		t.Fatal(err)
	}
	if err := saveKeystore(secret); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := loadKeystoreSecret(); got != secret {
		t.Fatal("failed to reload keystore")
	}
	fileInfo, _ := os.Stat(path)
	dirInfo, _ := os.Stat(filepath.Dir(path))
	if fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %o", fileInfo.Mode().Perm())
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %o", dirInfo.Mode().Perm())
	}
}

func TestLegacyPlaintextKeystoreMigratesOnRead(t *testing.T) {
	path := withTestKeystore(t)
	secret, _, err := newAgentSecret()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	legacy, _ := json.Marshal(legacyKeystore{Secret: secret, Created: "legacy"})
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}

	if got := loadKeystoreSecret(); got != secret {
		t.Fatal("legacy migration lost secret")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secret) || strings.Contains(string(raw), `"secret"`) {
		t.Fatal("legacy plaintext remained after migration")
	}
}

func TestPBKDF2IsDeterministicAndSaltSensitive(t *testing.T) {
	a := pbkdf2SHA256([]byte("password"), []byte("0123456789abcdef"), 1000, 32)
	b := pbkdf2SHA256([]byte("password"), []byte("0123456789abcdef"), 1000, 32)
	c := pbkdf2SHA256([]byte("password"), []byte("fedcba9876543210"), 1000, 32)
	if string(a) != string(b) {
		t.Fatal("PBKDF2 is not deterministic")
	}
	if string(a) == string(c) {
		t.Fatal("PBKDF2 ignored salt")
	}
}
