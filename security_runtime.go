package main

import "os"

const pinnedMCPCommand = "npx -y --ignore-scripts @aifinpay/mcp@1.4.1"

// The passphrase is captured once into process memory and immediately removed
// from the process environment. exec.Cmd inherits os.Environ(), so leaving it
// there would disclose the keystore decryption secret to the MCP child and any
// subprocess it starts. keystorePassphrase() reads this cached value.
var processKeystorePassphrase string

// Security bootstrap for the CLI→MCP boundary.
//
// The historical default executed an unpinned `npx -y @aifinpay/mcp` while the
// wallet secret was present in the child environment. Pin the exact audited MCP
// package and disable npm lifecycle scripts. Custom commands are ignored unless
// the operator explicitly opts in.
func init() {
	processKeystorePassphrase = os.Getenv(envKeystorePwd)
	_ = os.Unsetenv(envKeystorePwd)

	custom := os.Getenv("AIFINPAY_MCP_CMD")
	if custom == "" || os.Getenv("AIFINPAY_ALLOW_CUSTOM_MCP") != "1" {
		_ = os.Setenv("AIFINPAY_MCP_CMD", pinnedMCPCommand)
	}
}
