package main

// Persistent agent identity.
//
// Every command spawns a fresh `@aifinpay/mcp` subprocess, and that server
// mints a throwaway agent when no secret is supplied. So `aifinpay address`
// used to print one wallet while `aifinpay call` paid from a different one,
// and any balance sent to the printed address was stranded the moment the
// process exited.
//
// The fix is a keystore on disk plus a refusal to move money under an
// ephemeral identity:
//
//	aifinpay init            create and persist an identity
//	aifinpay import <secret> adopt an existing base58 secret
//	aifinpay whoami          show which identity is in effect, and from where
//
// Precedence: AIFINPAY_AGENT_SECRET (env) > keystore file. The resolved secret
// is injected into the subprocess environment so every command uses the same
// wallet.

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	envSecret   = "AIFINPAY_AGENT_SECRET"
	envHome     = "AIFINPAY_HOME"
	keystoreDir = ".aifinpay"
	keystoreFil = "agent.json"
)

// Tools that move funds or bind identity. Running these under a throwaway
// wallet either burns money from an unrecoverable key or writes a binding that
// cannot be reproduced, so they require a persistent identity.
var fundedTools = map[string]bool{
	"agent_call":       true,
	"payable_fetch":    true,
	"agent_claim_self": true,
}

type keystore struct {
	Secret  string `json:"secret"`  // base58, 64-byte ed25519 private key
	Created string `json:"created"` // RFC3339, informational
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

// loadKeystoreSecret returns the stored secret, or "" when none is saved.
func loadKeystoreSecret() string {
	raw, err := os.ReadFile(keystorePath())
	if err != nil {
		return ""
	}
	var ks keystore
	if json.Unmarshal(raw, &ks) != nil {
		return ""
	}
	return strings.TrimSpace(ks.Secret)
}

// resolveSecret applies the env > keystore precedence.
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
	path := keystorePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	body, err := json.MarshalIndent(keystore{Secret: secret, Created: time.Now().UTC().Format(time.RFC3339)}, "", "  ")
	if err != nil {
		return err
	}
	// 0600: the file is a private key.
	return os.WriteFile(path, append(body, '\n'), 0o600)
}

// newAgentSecret generates a fresh ed25519 identity. Go's private key is
// seed||public — the same 64-byte layout tweetnacl (and therefore the SDK)
// expects, so the base58 form is interchangeable with SDK-generated secrets.
func newAgentSecret() (secret, pubkey string, err error) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return "", "", err
	}
	return base58Encode(priv), base58Encode(pub), nil
}

// validateSecret checks that a base58 string decodes to a usable agent key and
// returns the corresponding public key.
func validateSecret(secret string) (pubkey string, err error) {
	raw, err := base58Decode(strings.TrimSpace(secret))
	if err != nil {
		return "", fmt.Errorf("not valid base58: %w", err)
	}
	switch len(raw) {
	case ed25519.PrivateKeySize: // 64 — seed||public, the format the SDK emits
		return base58Encode(ed25519.PrivateKey(raw).Public().(ed25519.PublicKey)), nil
	case ed25519.SeedSize:
		// 32 bytes is ambiguous: it could be a bare seed, but it is far more
		// often a PUBLIC key pasted by mistake. Deriving an identity from a
		// pubkey-as-seed would silently produce a different wallet and strand
		// whatever the user funds, so refuse rather than guess.
		return "", fmt.Errorf(
			"got 32 bytes — that is a seed or, more likely, a public key. " +
				"Supply the 64-byte secret the SDK prints (agent.secret_b58 / agent.secretB58)")
	default:
		return "", fmt.Errorf("expected a 64-byte agent secret, got %d bytes", len(raw))
	}
}

// requireIdentity is called before any funded tool. It refuses to continue
// under a throwaway wallet rather than silently spending from a key the user
// can never recover.
func requireIdentity(tool string) {
	if !fundedTools[tool] {
		return
	}
	if secret, _ := resolveSecret(); secret != "" {
		return
	}
	fail(exitAuth, `no agent identity configured — refusing to run %q with a throwaway wallet.

Each command starts a fresh MCP server, so an ephemeral agent would differ
from the one `+"`aifinpay address`"+` printed, and any funds sent to it would be
lost when the process exits.

  aifinpay init             create and save an identity
  aifinpay import <secret>  use an existing base58 secret
  %s=<secret>               one-off, without saving

Read-only commands (quote, quote-split) work without an identity.`, tool, envSecret)
}

// ── commands ────────────────────────────────────────────────────────────────

func cmdInit(args []string) {
	fs := newFlagSet("aifinpay init")
	fs.usageLine = "aifinpay init [--force]"
	var force bool
	fs.BoolVar(&force, "force", false, "overwrite an existing keystore")
	fs.parse(args)

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
		fail(exitInput, "cannot write %s: %v", keystorePath(), err)
	}
	fmt.Printf("Agent identity created.\n\n  agent:    %s\n  keystore: %s (0600)\n\n"+
		"Back up that file — it is the only copy of the key.\n"+
		"Run `aifinpay address` for the funding addresses.\n", pub, keystorePath())
}

func cmdImport(args []string) {
	fs := newFlagSet("aifinpay import")
	fs.usageLine = "aifinpay import <base58-secret> [--force]"
	var force bool
	fs.BoolVar(&force, "force", false, "overwrite an existing keystore")
	fs.parse(args)

	rest := fs.args()
	if len(rest) != 1 {
		fail(exitInput, "usage: aifinpay import <base58-secret> [--force]")
	}
	pub, err := validateSecret(rest[0])
	if err != nil {
		fail(exitInput, "invalid agent secret: %v", err)
	}
	if existing := loadKeystoreSecret(); existing != "" && !force {
		fail(exitInput, "an identity already exists at %s — pass --force to replace it.", keystorePath())
	}
	if err := saveKeystore(strings.TrimSpace(rest[0])); err != nil {
		fail(exitInput, "cannot write %s: %v", keystorePath(), err)
	}
	fmt.Printf("Imported agent %s into %s (0600).\n", pub, keystorePath())
}

func cmdWhoami(args []string) {
	fs := newFlagSet("aifinpay whoami")
	fs.usageLine = "aifinpay whoami"
	fs.parse(args)

	secret, source := resolveSecret()
	if secret == "" {
		fmt.Printf("No identity configured.\nRun `aifinpay init` to create one, or set %s.\n", envSecret)
		os.Exit(exitAuth)
	}
	pub, err := validateSecret(secret)
	if err != nil {
		fail(exitInput, "configured secret is invalid: %v", err)
	}
	fmt.Printf("agent:  %s\nsource: %s\n\nRun `aifinpay address` for the Solana and Polygon funding addresses.\n", pub, source)
}

// ── base58 (Bitcoin alphabet) ───────────────────────────────────────────────
//
// Implemented here so the CLI keeps a zero-dependency stdlib build.

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
	// Leading zero bytes become leading '1's.
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
	// Restore leading zero bytes encoded as '1'.
	var leading int
	for _, r := range input {
		if r != rune(b58Alphabet[0]) {
			break
		}
		leading++
	}
	return append(make([]byte, leading), decoded...), nil
}
