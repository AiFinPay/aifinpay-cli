package main

// Persistent agent identity.
//
// Security invariants:
//   - private keys are never stored as plaintext;
//   - the on-disk keystore is AES-256-GCM authenticated ciphertext;
//   - the encryption key is derived with PBKDF2-HMAC-SHA256;
//   - keystore directory/file permissions are repaired on every read/write;
//   - importing a key never accepts the secret on the process command line.
//
// AIFINPAY_KEYSTORE_PASSPHRASE is intentionally consumed only by the CLI and
// is stripped before the MCP child is launched (see main.go). Production
// operators should inject it from their secret manager rather than shell
// history.

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	envSecret      = "AIFINPAY_AGENT_SECRET"
	envHome        = "AIFINPAY_HOME"
	envKeystorePwd = "AIFINPAY_KEYSTORE_PASSPHRASE"
	keystoreDir    = ".aifinpay"
	keystoreFil    = "agent.json"
	keystoreV2     = 2
	pbkdf2Rounds   = 600_000
)

var fundedTools = map[string]bool{
	"agent_call":       true,
	"payable_fetch":    true,
	"agent_claim_self": true,
}

// encryptedKeystore is the only format written by this release.
// AES-GCM authenticates the ciphertext as well as encrypting it.
type encryptedKeystore struct {
	Version    int    `json:"version"`
	KDF        string `json:"kdf"`
	Iterations int    `json:"iterations"`
	Salt       string `json:"salt"`
	Cipher     string `json:"cipher"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
	Created    string `json:"created"`
}

// legacyKeystore exists only so an old plaintext file can be migrated in-place
// once the operator supplies a passphrase. It is never written.
type legacyKeystore struct {
	Secret  string `json:"secret"`
	Created string `json:"created"`
}

func keystorePath() string {
	if h := strings.TrimSpace(os.Getenv(envHome)); h != "" {
		return filepath.Join(h, keystoreFil)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(keystoreDir, keystoreFil)
	}
	return filepath.Join(home, keystoreDir, keystoreFil)
}

func keystorePassphrase() (string, error) {
	pass := os.Getenv(envKeystorePwd)
	if len(pass) < 12 {
		return "", fmt.Errorf("%s must be set to at least 12 characters", envKeystorePwd)
	}
	return pass, nil
}

func repairKeystorePermissions(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		if err := os.Chmod(path, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// loadKeystoreSecret decrypts the persistent identity. A legacy plaintext
// keystore is migrated atomically to v2 as soon as a passphrase is available.
func loadKeystoreSecret() string {
	path := keystorePath()
	if err := repairKeystorePermissions(path); err != nil {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	var enc encryptedKeystore
	if json.Unmarshal(raw, &enc) == nil && enc.Version == keystoreV2 {
		pass, err := keystorePassphrase()
		if err != nil {
			return ""
		}
		secret, err := decryptKeystore(enc, pass)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(secret)
	}

	// One-way migration from the old plaintext {secret,created} file.
	var legacy legacyKeystore
	if json.Unmarshal(raw, &legacy) == nil && strings.TrimSpace(legacy.Secret) != "" {
		pass, err := keystorePassphrase()
		if err != nil {
			return ""
		}
		secret := strings.TrimSpace(legacy.Secret)
		if _, err := validateSecret(secret); err != nil {
			return ""
		}
		if err := saveKeystoreWithPassphrase(secret, pass); err != nil {
			return ""
		}
		return secret
	}
	return ""
}

func resolveSecret() (secret, source string) {
	if s := strings.TrimSpace(os.Getenv(envSecret)); s != "" {
		return s, "environment (" + envSecret + ")"
	}
	if s := loadKeystoreSecret(); s != "" {
		return s, keystorePath()
	}
	return "", ""
}

func saveKeystore(secret string) error {
	pass, err := keystorePassphrase()
	if err != nil {
		return err
	}
	return saveKeystoreWithPassphrase(secret, pass)
}

func saveKeystoreWithPassphrase(secret, pass string) error {
	path := keystorePath()
	if err := repairKeystorePermissions(path); err != nil {
		return err
	}

	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return err
	}
	key := pbkdf2SHA256([]byte(pass), salt, pbkdf2Rounds, 32)
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(strings.TrimSpace(secret)), nil)
	ks := encryptedKeystore{
		Version:    keystoreV2,
		KDF:        "pbkdf2-hmac-sha256",
		Iterations: pbkdf2Rounds,
		Salt:       base64.RawStdEncoding.EncodeToString(salt),
		Cipher:     "aes-256-gcm",
		Nonce:      base64.RawStdEncoding.EncodeToString(nonce),
		Ciphertext: base64.RawStdEncoding.EncodeToString(ciphertext),
		Created:    time.Now().UTC().Format(time.RFC3339),
	}
	body, err := json.MarshalIndent(ks, "", "  ")
	if err != nil {
		return err
	}

	// Atomic replacement: a crash never leaves half a private-key file.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".agent-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(body, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return repairKeystorePermissions(path)
}

func decryptKeystore(ks encryptedKeystore, pass string) (string, error) {
	if ks.Version != keystoreV2 || ks.KDF != "pbkdf2-hmac-sha256" || ks.Cipher != "aes-256-gcm" {
		return "", fmt.Errorf("unsupported keystore format")
	}
	if ks.Iterations < 100_000 || ks.Iterations > 5_000_000 {
		return "", fmt.Errorf("invalid KDF iteration count")
	}
	salt, err := base64.RawStdEncoding.DecodeString(ks.Salt)
	if err != nil || len(salt) < 16 {
		return "", fmt.Errorf("invalid keystore salt")
	}
	nonce, err := base64.RawStdEncoding.DecodeString(ks.Nonce)
	if err != nil {
		return "", fmt.Errorf("invalid keystore nonce")
	}
	ciphertext, err := base64.RawStdEncoding.DecodeString(ks.Ciphertext)
	if err != nil {
		return "", fmt.Errorf("invalid keystore ciphertext")
	}
	key := pbkdf2SHA256([]byte(pass), salt, ks.Iterations, 32)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(nonce) != gcm.NonceSize() {
		return "", fmt.Errorf("invalid keystore nonce length")
	}
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("keystore authentication failed")
	}
	return string(plain), nil
}

// Minimal PBKDF2-HMAC-SHA256 implementation so the CLI keeps a stdlib-only
// build while still using a real salted password KDF.
func pbkdf2SHA256(password, salt []byte, iterations, keyLen int) []byte {
	hLen := sha256.Size
	blocks := (keyLen + hLen - 1) / hLen
	out := make([]byte, 0, blocks*hLen)
	for block := 1; block <= blocks; block++ {
		mac := hmac.New(sha256.New, password)
		_, _ = mac.Write(salt)
		var counter [4]byte
		binary.BigEndian.PutUint32(counter[:], uint32(block))
		_, _ = mac.Write(counter[:])
		u := mac.Sum(nil)
		t := append([]byte(nil), u...)
		for i := 1; i < iterations; i++ {
			mac = hmac.New(sha256.New, password)
			_, _ = mac.Write(u)
			u = mac.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		out = append(out, t...)
	}
	return out[:keyLen]
}

func newAgentSecret() (secret, pubkey string, err error) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return "", "", err
	}
	return base58Encode(priv), base58Encode(pub), nil
}

func validateSecret(secret string) (pubkey string, err error) {
	raw, err := base58Decode(strings.TrimSpace(secret))
	if err != nil {
		return "", fmt.Errorf("not valid base58: %w", err)
	}
	switch len(raw) {
	case ed25519.PrivateKeySize:
		return base58Encode(ed25519.PrivateKey(raw).Public().(ed25519.PublicKey)), nil
	case ed25519.SeedSize:
		return "", fmt.Errorf(
			"got 32 bytes — supply the 64-byte secret emitted by the SDK")
	default:
		return "", fmt.Errorf("expected a 64-byte agent secret, got %d bytes", len(raw))
	}
}

func requireIdentity(tool string) {
	if !fundedTools[tool] {
		return
	}
	if secret, _ := resolveSecret(); secret != "" {
		return
	}
	fail(exitAuth, `no decryptable agent identity configured — refusing to run %q.

For a persistent encrypted keystore:
  export %s='<strong passphrase from your secret manager>'
  aifinpay init

For a one-off identity:
  %s=<secret>

The CLI never stores a plaintext private key.`, tool, envKeystorePwd, envSecret)
}

// ── commands ────────────────────────────────────────────────────────────────

func cmdInit(args []string) {
	fs := newFlagSet("aifinpay init")
	fs.usageLine = "aifinpay init [--force]"
	var force bool
	fs.BoolVar(&force, "force", false, "overwrite an existing keystore")
	fs.parse(args)

	if _, err := keystorePassphrase(); err != nil {
		fail(exitInput, "%v", err)
	}
	if existing := loadKeystoreSecret(); existing != "" && !force {
		pub, err := validateSecret(existing)
		if err != nil {
			fail(exitInput, "keystore at %s is unreadable: %v (use --force to replace)", keystorePath(), err)
		}
		fail(exitInput, "an identity already exists at %s (agent %s) — pass --force to replace it.\n"+
			"Replacing it strands any funds held by the current agent.", keystorePath(), pub)
	}

	secret, pub, err := newAgentSecret()
	if err != nil {
		fail(exitNetwork, "key generation failed: %v", err)
	}
	if err := saveKeystore(secret); err != nil {
		fail(exitInput, "cannot write encrypted %s: %v", keystorePath(), err)
	}
	fmt.Printf("Agent identity created.\n\n  agent:    %s\n  keystore: %s (encrypted, mode 0600)\n\n"+
		"Back up the encrypted file and keep %s in your secret manager.\n"+
		"Run `aifinpay address` for the funding addresses.\n", pub, keystorePath(), envKeystorePwd)
}

func cmdImport(args []string) {
	fs := newFlagSet("aifinpay import")
	fs.usageLine = "aifinpay import [--force]  # reads the base58 secret from stdin"
	var force bool
	fs.BoolVar(&force, "force", false, "overwrite an existing keystore")
	fs.parse(args)

	if len(fs.args()) != 0 {
		fail(exitInput, "private keys are not accepted as command-line arguments; pipe the secret on stdin")
	}
	if _, err := keystorePassphrase(); err != nil {
		fail(exitInput, "%v", err)
	}
	fmt.Fprint(os.Stderr, "Paste the 64-byte base58 agent secret, then press Enter: ")
	var secret string
	if _, err := fmt.Fscan(os.Stdin, &secret); err != nil {
		fail(exitInput, "cannot read agent secret from stdin: %v", err)
	}
	secret = strings.TrimSpace(secret)
	pub, err := validateSecret(secret)
	if err != nil {
		fail(exitInput, "invalid agent secret: %v", err)
	}
	if existing := loadKeystoreSecret(); existing != "" && !force {
		fail(exitInput, "an identity already exists at %s — pass --force to replace it.", keystorePath())
	}
	if err := saveKeystore(secret); err != nil {
		fail(exitInput, "cannot write encrypted %s: %v", keystorePath(), err)
	}
	fmt.Printf("Imported agent %s into encrypted keystore %s (0600).\n", pub, keystorePath())
}

func cmdWhoami(args []string) {
	fs := newFlagSet("aifinpay whoami")
	fs.usageLine = "aifinpay whoami"
	fs.parse(args)

	secret, source := resolveSecret()
	if secret == "" {
		fmt.Printf("No decryptable identity configured.\nSet %s for the encrypted keystore, run `aifinpay init`, or set %s.\n", envKeystorePwd, envSecret)
		os.Exit(exitAuth)
	}
	pub, err := validateSecret(secret)
	if err != nil {
		fail(exitInput, "configured secret is invalid: %v", err)
	}
	fmt.Printf("agent:  %s\nsource: %s\n\nRun `aifinpay address` for funding addresses.\n", pub, source)
}

// ── base58 (Bitcoin alphabet) ───────────────────────────────────────────────

const b58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

func base58Encode(input []byte) string {
	if len(input) == 0 {
		return ""
	}
	x := new(big.Int).SetBytes(input)
	radix := big.NewInt(58)
	zero := big.NewInt(0)
	mod := new(big.Int)

	var out []byte
	for x.Cmp(zero) > 0 {
		x.DivMod(x, radix, mod)
		out = append(out, b58Alphabet[mod.Int64()])
	}
	for _, b := range input {
		if b != 0 {
			break
		}
		out = append(out, b58Alphabet[0])
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}

func base58Decode(input string) ([]byte, error) {
	if input == "" {
		return nil, fmt.Errorf("empty string")
	}
	x := big.NewInt(0)
	radix := big.NewInt(58)
	for _, r := range input {
		idx := strings.IndexRune(b58Alphabet, r)
		if idx < 0 {
			return nil, fmt.Errorf("invalid character %q", r)
		}
		x.Mul(x, radix)
		x.Add(x, big.NewInt(int64(idx)))
	}
	decoded := x.Bytes()
	var leading int
	for _, r := range input {
		if r != rune(b58Alphabet[0]) {
			break
		}
		leading++
	}
	return append(make([]byte, leading), decoded...), nil
}
