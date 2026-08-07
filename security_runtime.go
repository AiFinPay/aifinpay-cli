package main

import "os"

const pinnedMCPCommand = "npx -y --ignore-scripts @aifinpay/mcp@1.4.1"

// Security bootstrap for the CLI→MCP boundary.
//
// The historical default executed an unpinned `npx -y @aifinpay/mcp` while the
// wallet secret was present in the child environment. A registry update could
// therefore change the code that received the key without any CLI change.
//
// Pin the exact audited MCP version and disable npm lifecycle scripts. Custom
// commands are ignored unless the operator explicitly opts in; this keeps a
// poisoned AIFINPAY_MCP_CMD from silently receiving the wallet key.
//
// The keystore passphrase is still present in the parent environment until
// main.go's child-environment filter is added. Do not mark H-3 closed on this
// commit; pinning is containment, not the final secret-transport design.
func init() {
	custom := os.Getenv("AIFINPAY_MCP_CMD")
	if custom == "" || os.Getenv("AIFINPAY_ALLOW_CUSTOM_MCP") != "1" {
		_ = os.Setenv("AIFINPAY_MCP_CMD", pinnedMCPCommand)
	}
}
