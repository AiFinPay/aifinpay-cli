// aifinpay — agent-first CLI for AiFinPay ("Stripe for AI agents").
//
// It wraps the official @aifinpay/mcp server (launched under the hood via
// `npx -y @aifinpay/mcp`) and exposes its audited tools as a clean CLI
// command. Stdout is always JSON (the machine contract); --output table is
// for humans. Zero interactive prompts, semantic exit codes, stdin piping.
//
// Go stdlib only — no third-party dependencies.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

const version = "1.1.0"

// Exit codes (the machine contract — documented in README).
const (
	exitOK       = 0
	exitInput    = 1 // missing/invalid flag
	exitAuth     = 2 // bad/missing agent secret
	exitNotFound = 3 // unknown provider / not in registry
	exitServer   = 4 // server-side tool error
	exitNetwork  = 5 // MCP server failed to launch / connect
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		usage(os.Stderr)
		os.Exit(exitInput)
	}
	switch args[0] {
	case "-h", "--help", "help":
		usage(os.Stdout)
		os.Exit(exitOK)
	case "-v", "--version", "version":
		fmt.Println("aifinpay " + version)
		os.Exit(exitOK)
	}

	cmd := args[0]
	rest := args[1:]
	c, ok := commands[cmd]
	if !ok {
		fail(exitInput, "unknown command %q (try `aifinpay --help`)", cmd)
	}
	c.run(rest)
}

// ── command table ────────────────────────────────────────────────────────────

type command struct {
	tool    string
	summary string
	run     func(args []string)
}

var commands map[string]command

func init() {
	commands = map[string]command{
		"address":     {"agent_address", "Show the agent's on-chain identity (Solana + Polygon).", cmdAddress},
		"call":        {"agent_call", "Pay & call an AiFinPay-registered provider (exa, io-net, venice, …).", cmdCall},
		"quote":       {"agent_quote", "Inspect a 402 challenge for a URL without paying.", cmdQuote},
		"fetch":       {"payable_fetch", "Fetch any URL, auto-paying an HTTP 402 challenge (x402).", cmdFetch},
		"pay-split":   {"pay_with_split", "Get on-chain instructions for a fee-on-top atomic 3-way payment.", cmdPaySplit},
		"quote-split": {"quote_split", "Compute the fee-on-top breakdown (view only, no payment).", cmdQuoteSplit},

		// Identity management. Without a persisted key every command would run
		// under a different throwaway wallet — see keystore.go.
		"init":   {"", "Create and save a persistent agent identity.", cmdInit},
		"import": {"", "Adopt an existing base58 agent secret.", cmdImport},
		"whoami": {"", "Show which agent identity is in effect.", cmdWhoami},
	}
}

// ── per-command flag parsing ─────────────────────────────────────────────────

// parsed holds global flags shared by every command.
type parsed struct {
	output string // json | table
}

// newFS builds a flagset with the shared --output flag and a clean usage.
func newFS(name, synopsis string) (*flagSet, *parsed) {
	g := &parsed{output: "json"}
	fs := newFlagSet("aifinpay " + name)
	fs.usageLine = synopsis
	fs.StringVar(&g.output, "output", "json", "output format: json|table")
	return fs, g
}

func cmdAddress(args []string) {
	fs, g := newFS("address", "aifinpay address [--output json|table]")
	fs.parse(args)
	emit(callTool("agent_address", map[string]any{}), g)
}

func cmdCall(args []string) {
	fs, g := newFS("call", "aifinpay call --provider <name> [--data <json> | --stdin] [--method M] [--cost USD]")
	var provider, data, method string
	var cost float64
	var useStdin bool
	fs.StringVar(&provider, "provider", "", "registered provider name (required)")
	fs.StringVar(&data, "data", "", "request body as a JSON string")
	fs.BoolVar(&useStdin, "stdin", false, "read the JSON request body from stdin")
	fs.StringVar(&method, "method", "", "HTTP method (default POST)")
	fs.Float64Var(&cost, "cost", 0, "override price cap (USD)")
	fs.parse(args)
	if provider == "" {
		fail(exitInput, "--provider is required (e.g. exa, io-net, venice)")
	}
	a := map[string]any{"provider": provider}
	if body := jsonBody(data, useStdin); body != nil {
		a["body"] = body
	}
	if method != "" {
		a["method"] = method
	}
	if fs.wasSet("cost") {
		a["cost"] = cost
	}
	emit(callTool("agent_call", a), g)
}

func cmdQuote(args []string) {
	fs, g := newFS("quote", "aifinpay quote --url <https> [--method M]")
	var url, method string
	fs.StringVar(&url, "url", "", "target URL (required)")
	fs.StringVar(&method, "method", "", "HTTP method")
	fs.parse(args)
	if url == "" {
		fail(exitInput, "--url is required")
	}
	a := map[string]any{"url": url}
	if method != "" {
		a["method"] = method
	}
	emit(callTool("agent_quote", a), g)
}

func cmdFetch(args []string) {
	fs, g := newFS("fetch", "aifinpay fetch --url <https> [--body S | --stdin] [--method M] [--headers k:v,k:v] [--max-amount-usd N] [--facilitator URL]")
	var url, body, method, headers, facilitator string
	var maxUSD float64
	var useStdin bool
	fs.StringVar(&url, "url", "", "target URL (required)")
	fs.StringVar(&body, "body", "", "raw request body")
	fs.BoolVar(&useStdin, "stdin", false, "read the raw request body from stdin")
	fs.StringVar(&method, "method", "", "HTTP method")
	fs.StringVar(&headers, "headers", "", "extra headers as k:v,k:v")
	fs.Float64Var(&maxUSD, "max-amount-usd", 0, "hard cap on the auto-payment (USD)")
	fs.StringVar(&facilitator, "facilitator", "", "x402 facilitator URL override")
	fs.parse(args)
	if url == "" {
		fail(exitInput, "--url is required")
	}
	a := map[string]any{"url": url}
	if useStdin {
		a["body"] = readStdin()
	} else if body != "" {
		a["body"] = body
	}
	if method != "" {
		a["method"] = method
	}
	if h := parseHeaders(headers); len(h) > 0 {
		a["headers"] = h
	}
	if fs.wasSet("max-amount-usd") {
		a["max_amount_usd"] = maxUSD
	}
	if facilitator != "" {
		a["facilitator"] = facilitator
	}
	emit(callTool("payable_fetch", a), g)
}

func cmdPaySplit(args []string) {
	fs, g := newFS("pay-split", "aifinpay pay-split --chain <solana|polygon> --merchant-wallet W --merchant-amount A --order-id ID [--fee-recipient R]")
	var chain, wallet, amount, orderID, feeRecipient string
	fs.StringVar(&chain, "chain", "", "settlement chain: solana|polygon (required)")
	fs.StringVar(&wallet, "merchant-wallet", "", "merchant wallet address (required)")
	fs.StringVar(&amount, "merchant-amount", "", "merchant amount in chain units (required)")
	fs.StringVar(&orderID, "order-id", "", "off-chain reference, max 64 chars (required)")
	fs.StringVar(&feeRecipient, "fee-recipient", "", "optional referral/creator fee recipient")
	fs.parse(args)
	requireChain(chain)
	for k, v := range map[string]string{"merchant-wallet": wallet, "merchant-amount": amount, "order-id": orderID} {
		if v == "" {
			fail(exitInput, "--%s is required", k)
		}
	}
	a := map[string]any{"chain": chain, "merchant_wallet": wallet, "merchant_amount": amount, "order_id": orderID}
	if feeRecipient != "" {
		a["fee_recipient"] = feeRecipient
	}
	emit(callTool("pay_with_split", a), g)
}

func cmdQuoteSplit(args []string) {
	fs, g := newFS("quote-split", "aifinpay quote-split --chain <solana|polygon> --merchant-amount A [--include-creator]")
	var chain, amount string
	var includeCreator bool
	fs.StringVar(&chain, "chain", "", "chain: solana|polygon (required)")
	fs.StringVar(&amount, "merchant-amount", "", "merchant amount in chain units (required)")
	fs.BoolVar(&includeCreator, "include-creator", false, "include the optional 0.01% creator fee")
	fs.parse(args)
	requireChain(chain)
	if amount == "" {
		fail(exitInput, "--merchant-amount is required")
	}
	emit(callTool("quote_split", map[string]any{
		"chain": chain, "merchant_amount": amount, "include_creator": includeCreator,
	}), g)
}

func requireChain(chain string) {
	if chain != "solana" && chain != "polygon" {
		fail(exitInput, "--chain must be 'solana' or 'polygon'")
	}
}

// ── MCP stdio client ─────────────────────────────────────────────────────────

// callTool launches the @aifinpay/mcp server, performs the MCP handshake,
// calls one tool, and returns its text payload. Exits with the right code on
// any failure (network, auth, not-found, server error).
func callTool(tool string, arguments map[string]any) string {
	timeout := 30 * time.Second
	if ms := envInt("AIFINPAY_TIMEOUT_MS"); ms > 0 {
		timeout = time.Duration(ms) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Refuse to spend from a wallet that disappears when this process exits.
	requireIdentity(tool)

	parts := mcpCommand()
	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	// Do not inherit the full parent environment. Cloud credentials, database
	// URLs, keystore passphrases and unrelated API keys must never be exposed to
	// npm/the MCP child. mcpChildEnv keeps only execution/proxy state plus the
	// explicit AiFinPay runtime policy variables.
	cmd.Env = mcpChildEnv()
	// The audited MCP process itself needs the agent signing secret. Pass exactly
	// that one credential after the environment has been filtered; the default
	// command is version-pinned and npm lifecycle scripts are disabled.
	secret := os.Getenv(envSecret)
	if secret == "" {
		secret = loadKeystoreSecret()
	}
	if secret != "" {
		cmd.Env = append(cmd.Env, envSecret+"="+secret)
	}
	cmd.Stderr = os.Stderr // the MCP server logs to stderr; surface it

	stdin, err := cmd.StdinPipe()
	if err != nil {
		fail(exitNetwork, "cannot open MCP stdin: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fail(exitNetwork, "cannot open MCP stdout: %v", err)
	}
	if err := cmd.Start(); err != nil {
		fail(exitNetwork, "failed to launch MCP server (%s): %v — is Node.js installed?", strings.Join(parts, " "), err)
	}
	defer func() { _ = cmd.Process.Kill() }()

	r := bufio.NewReaderSize(stdout, 1<<20)
	send := func(v any) {
		b, _ := json.Marshal(v)
		_, _ = stdin.Write(append(b, '\n'))
	}

	// 1) initialize
	send(rpcReq(1, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "aifinpay-cli", "version": version},
	}))
	if _, err := readResult(r, 1); err != nil {
		fail(exitNetwork, "MCP initialize failed: %v", err)
	}
	// 2) initialized notification
	send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})

	// 3) tools/call
	send(rpcReq(2, "tools/call", map[string]any{"name": tool, "arguments": arguments}))
	res, err := readResult(r, 2)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			fail(exitNetwork, "MCP call timed out after %s", timeout)
		}
		fail(exitNetwork, "MCP call failed: %v", err)
	}

	text, isErr := toolText(res)
	if isErr {
		fail(classifyError(text), "%s", text)
	}
	return text
}

func mcpCommand() []string {
	if c := strings.TrimSpace(os.Getenv("AIFINPAY_MCP_CMD")); c != "" {
		return strings.Fields(c)
	}
	return []string{"npx", "-y", "@aifinpay/mcp"}
}

func rpcReq(id int, method string, params any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
}

// readResult reads newline-delimited JSON-RPC messages until it sees a
// response with the given id, returning its `result` object.
func readResult(r *bufio.Reader, id int) (map[string]any, error) {
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			var msg map[string]any
			if json.Unmarshal(line, &msg) == nil {
				if fid, ok := numID(msg["id"]); ok && fid == id {
					if e, ok := msg["error"].(map[string]any); ok {
						return nil, fmt.Errorf("%v", e["message"])
					}
					if res, ok := msg["result"].(map[string]any); ok {
						return res, nil
					}
					return map[string]any{}, nil
				}
				// otherwise: a notification or another id — keep reading
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil, fmt.Errorf("MCP server closed the stream before responding")
			}
			return nil, err
		}
	}
}

// toolText extracts the first text content block + the isError flag.
func toolText(res map[string]any) (string, bool) {
	isErr, _ := res["isError"].(bool)
	if content, ok := res["content"].([]any); ok {
		for _, c := range content {
			if m, ok := c.(map[string]any); ok {
				if t, _ := m["type"].(string); t == "text" {
					if s, ok := m["text"].(string); ok {
						return s, isErr
					}
				}
			}
		}
	}
	b, _ := json.Marshal(res)
	return string(b), isErr
}

// classifyError maps a tool error message to an exit code.
func classifyError(msg string) int {
	m := strings.ToLower(msg)
	switch {
	case strings.Contains(m, "not in registry"), strings.Contains(m, "not found"), strings.Contains(m, "unknown provider"):
		return exitNotFound
	case strings.Contains(m, "secret"), strings.Contains(m, "unauthor"), strings.Contains(m, "auth"):
		return exitAuth
	default:
		return exitServer
	}
}

// ── output ───────────────────────────────────────────────────────────────────

// emit prints the tool's payload. Default: pretty JSON to stdout (machine
// contract). --output table: a human-readable key/value or column table.
func emit(text string, g *parsed) {
	if g.output == "table" {
		printTable(text)
		os.Exit(exitOK)
	}
	// JSON: re-indent if it parses, else pass through verbatim.
	var v any
	if json.Unmarshal([]byte(text), &v) == nil {
		b, _ := json.MarshalIndent(v, "", "  ")
		fmt.Println(string(b))
	} else {
		fmt.Println(text)
	}
	os.Exit(exitOK)
}

func printTable(text string) {
	var v any
	if json.Unmarshal([]byte(text), &v) != nil {
		fmt.Println(text)
		return
	}
	switch t := v.(type) {
	case map[string]any:
		printKV(t)
	case []any:
		for i, row := range t {
			if i > 0 {
				fmt.Println()
			}
			if m, ok := row.(map[string]any); ok {
				printKV(m)
			} else {
				fmt.Printf("%v\n", row)
			}
		}
	default:
		fmt.Printf("%v\n", v)
	}
}

func printKV(m map[string]any) {
	keys := make([]string, 0, len(m))
	w := 0
	for k := range m {
		keys = append(keys, k)
		if len(k) > w {
			w = len(k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		val := m[k]
		switch val.(type) {
		case map[string]any, []any:
			b, _ := json.Marshal(val)
			fmt.Printf("%-*s  %s\n", w, k, string(b))
		default:
			fmt.Printf("%-*s  %v\n", w, k, val)
		}
	}
}

// ── small helpers ────────────────────────────────────────────────────────────

func jsonBody(data string, useStdin bool) any {
	raw := data
	if useStdin {
		raw = readStdin()
	}
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		fail(exitInput, "request body is not valid JSON: %v", err)
	}
	return v
}

func parseHeaders(s string) map[string]string {
	out := map[string]string{}
	for _, pair := range strings.Split(s, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		i := strings.IndexAny(pair, ":=")
		if i <= 0 {
			fail(exitInput, "bad header %q (want k:v)", pair)
		}
		out[strings.TrimSpace(pair[:i])] = strings.TrimSpace(pair[i+1:])
	}
	return out
}

func readStdin() string {
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		fail(exitInput, "failed to read stdin: %v", err)
	}
	return string(b)
}

func envInt(k string) int {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return 0
	}
	n := 0
	for _, r := range v {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func numID(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	}
	return 0, false
}

func fail(code int, format string, a ...any) {
	fmt.Fprintf(os.Stderr, "aifinpay: "+format+"\n", a...)
	os.Exit(code)
}

func usage(w io.Writer) {
	fmt.Fprintf(w, `aifinpay %s — agent-first CLI for AiFinPay ("Stripe for AI agents")

Pay-per-call x402 payments + on-chain settlement (Polygon / Solana). Wraps the
official @aifinpay/mcp server and exposes every tool as a CLI command.

USAGE
  aifinpay <command> [flags]

COMMANDS
`, version)
	names := make([]string, 0, len(commands))
	for n := range commands {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(w, "  %-12s %s\n", n, commands[n].summary)
	}
	fmt.Fprintf(w, `
GLOBAL FLAGS
  --output json|table   output format (default json; stdout is always the machine contract)
  -h, --help            show this help
  -v, --version         print version

GETTING STARTED
  aifinpay init         create a persistent agent identity (once)
  aifinpay address      show where to send funds
  aifinpay call ...     pay a provider

  Every command launches its own MCP server, so without a saved identity each
  one would use a DIFFERENT throwaway wallet and anything you funded would be
  unreachable. Commands that move funds refuse to run until an identity exists.

ENVIRONMENT
  AIFINPAY_AGENT_SECRET base58 agent secret (64-byte); overrides the keystore
  AIFINPAY_HOME         keystore directory (default ~/.aifinpay)
  AIFINPAY_BASE_URL     API base URL (default https://aifinpay.io)
  AIFINPAY_TIMEOUT_MS   per-call timeout in ms (default 30000)
  AIFINPAY_MAX_USD      hard cap (USD) per single payment
  AIFINPAY_MCP_CMD      override the MCP launch command (default: npx -y @aifinpay/mcp)

Requires Node.js on the host — the CLI launches the MCP server under the hood.
Docs: https://github.com/AiFinPay/aifinpay-cli
`)
}
