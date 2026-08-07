package main

import (
	"os"
	"strings"
)

const pinnedMCPCommand = "npx -y --ignore-scripts @aifinpay/mcp@1.4.1"

// Only variables required to locate executables, temporary/home directories,
// proxy transport, and explicit AiFinPay runtime policy are inherited by the
// MCP child. Parent cloud credentials, CI secrets, keystore passphrases,
// database tokens, SSH agents and unrelated API keys are deliberately absent.
var mcpInheritedEnv = map[string]struct{}{
	"PATH": {}, "HOME": {}, "USERPROFILE": {}, "APPDATA": {}, "LOCALAPPDATA": {},
	"TMPDIR": {}, "TMP": {}, "TEMP": {}, "SYSTEMROOT": {}, "WINDIR": {},
	"COMSPEC": {}, "PATHEXT": {},
	"HTTP_PROXY": {}, "HTTPS_PROXY": {}, "NO_PROXY": {},
	"http_proxy": {}, "https_proxy": {}, "no_proxy": {},
	"AIFINPAY_BASE_URL": {}, "AIFINPAY_TIMEOUT_MS": {}, "AIFINPAY_MAX_USD": {},
	"AIFINPAY_MATIC_USD": {}, "AIFINPAY_ETH_USD": {}, "AIFINPAY_BOT_USD": {},
	"AIFINPAY_XRP_USD": {}, "AIFINPAY_SOL_USD": {}, "AIFINPAY_AVAX_USD": {},
	"AIFINPAY_BNB_USD": {}, "AIFINPAY_SPEND_LEDGER_PATH": {},
}

func mcpChildEnv() []string {
	env := make([]string, 0, len(mcpInheritedEnv)+3)
	for _, entry := range os.Environ() {
		key := entry
		if i := strings.IndexByte(entry, '='); i >= 0 {
			key = entry[:i]
		}
		if _, ok := mcpInheritedEnv[key]; ok {
			env = append(env, entry)
		}
	}
	// npm itself should not execute lifecycle scripts when resolving the exact
	// pinned MCP package. These values are policy, not parent inheritance.
	env = append(env,
		"npm_config_ignore_scripts=true",
		"npm_config_update_notifier=false",
		"npm_config_fund=false",
	)
	return env
}

// Security bootstrap for the CLI→MCP boundary.
//
// The historical default executed an unpinned `npx -y @aifinpay/mcp` while the
// wallet secret was present in the child environment. A registry update could
// therefore change the code that received the key without any CLI change.
//
// Pin the exact audited MCP version and disable npm lifecycle scripts. Custom
// commands are ignored unless the operator explicitly opts in; this keeps a
// poisoned AIFINPAY_MCP_CMD from silently receiving the wallet key.
func init() {
	custom := os.Getenv("AIFINPAY_MCP_CMD")
	if custom == "" || os.Getenv("AIFINPAY_ALLOW_CUSTOM_MCP") != "1" {
		_ = os.Setenv("AIFINPAY_MCP_CMD", pinnedMCPCommand)
	}
}
