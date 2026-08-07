package main

import (
	"strings"
	"testing"
)

func envMap(entries []string) map[string]string {
	out := map[string]string{}
	for _, entry := range entries {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) == 2 {
			out[parts[0]] = parts[1]
		}
	}
	return out
}

func TestMCPChildEnvFiltersParentSecrets(t *testing.T) {
	t.Setenv("PATH", "/safe/bin")
	t.Setenv("HOME", "/home/test")
	t.Setenv("AIFINPAY_BASE_URL", "https://aifinpay.io")
	t.Setenv("AIFINPAY_MAX_USD", "0.10")
	t.Setenv("AIFINPAY_KEYSTORE_PASSPHRASE", "must-not-leak")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "must-not-leak")
	t.Setenv("DATABASE_URL", "must-not-leak")
	t.Setenv("OPENAI_API_KEY", "must-not-leak")
	t.Setenv("NODE_OPTIONS", "--require=/tmp/attacker.js")

	env := envMap(mcpChildEnv())
	if env["PATH"] != "/safe/bin" || env["HOME"] != "/home/test" {
		t.Fatalf("required execution environment was not preserved: %#v", env)
	}
	if env["AIFINPAY_BASE_URL"] != "https://aifinpay.io" || env["AIFINPAY_MAX_USD"] != "0.10" {
		t.Fatalf("explicit AiFinPay policy environment was not preserved: %#v", env)
	}
	for _, forbidden := range []string{
		"AIFINPAY_KEYSTORE_PASSPHRASE",
		"AWS_SECRET_ACCESS_KEY",
		"DATABASE_URL",
		"OPENAI_API_KEY",
		"NODE_OPTIONS",
		"AIFINPAY_AGENT_SECRET",
	} {
		if _, ok := env[forbidden]; ok {
			t.Fatalf("sensitive parent variable %s leaked into MCP child", forbidden)
		}
	}
	if env["npm_config_ignore_scripts"] != "true" {
		t.Fatal("npm lifecycle scripts must remain disabled")
	}
}
