# aifinpay — CLI for AiFinPay ("Stripe for AI Agents")

Agent-first command-line interface for **AiFinPay** — pay-per-call x402 payments
and on-chain settlement on **Polygon** and **Solana**. It wraps the official
[`@aifinpay/mcp`](https://www.npmjs.com/package/@aifinpay/mcp) server and exposes
every tool as a clean CLI command.

**Stdout is always JSON** (the machine contract). Use `--output table` for
human-readable output. Zero interactive prompts, semantic exit codes, stdin
piping — built for AI agents and scripts.

```
$ aifinpay quote-split --chain polygon --merchant-amount 1000000
{
  "chain": "polygon",
  "merchant": "1000000",
  "treasury_fee": "10000",
  "ip_creator_fee": "100",
  "total": "1010100",
  "protocol": "AiFinPay v5.3"
}
```

> **Requires Node.js** on the host — the CLI launches the AiFinPay MCP server
> under the hood via `npx -y @aifinpay/mcp` (no extra config; it auto-downloads
> on first run). Override the launcher with `AIFINPAY_MCP_CMD`.

## Install

```bash
# auto-detect platform (macOS / Linux / Windows, amd64 / arm64)
curl -fsSL https://raw.githubusercontent.com/AiFinPay/aifinpay-cli/main/install.sh | sh
```

Or grab a binary directly from
[**Releases**](https://github.com/AiFinPay/aifinpay-cli/releases/latest)
(`aifinpay-<os>-<arch>`), `chmod +x`, and put it on your `PATH`.

Or build from source (Go ≥ 1.21):

```bash
go install github.com/AiFinPay/aifinpay-cli@latest
```

## Quick start

```bash
# 1. (Optional) Load a stable agent identity. Without it, an ephemeral one is created.
export AIFINPAY_AGENT_SECRET="<base58-secret>"

# 2. Show the agent's on-chain addresses (fund either to enable payments)
aifinpay address --output table

# 3. Preview the fee-on-top breakdown for a payment — no auth, no payment
aifinpay quote-split --chain polygon --merchant-amount 1000000

# 4. Pay & call a registered provider (exa, io-net, venice, …)
aifinpay call --provider exa --data '{"query":"latest x402 spec"}'
```

## Command reference

| Command | Description | Key flags |
|---|---|---|
| `aifinpay address` | Show the agent's on-chain identity (Solana + Polygon). | — |
| `aifinpay call` | Pay & call an AiFinPay-registered provider. Auto-routes price, chain & bridge. | `--provider`\* `--data` `--stdin` `--method` `--cost` |
| `aifinpay quote` | Inspect a 402 challenge for a URL **without paying**. | `--url`\* `--method` |
| `aifinpay fetch` | Fetch any URL, auto-paying an HTTP 402 challenge (x402). | `--url`\* `--method` `--body` `--stdin` `--headers` `--max-amount-usd` `--facilitator` |
| `aifinpay claim` | Attach this agent to a user's AiFinPay account via a magic link. | `--magic-link`\* `--label` |
| `aifinpay pay-split` | Get on-chain instructions for a fee-on-top atomic 3-way payment. | `--chain`\* `--merchant-wallet`\* `--merchant-amount`\* `--order-id`\* `--fee-recipient` |
| `aifinpay quote-split` | Compute the fee-on-top breakdown (view only, no payment). | `--chain`\* `--merchant-amount`\* |

`*` = required. Global flags: `--output json|table`, `-h/--help`, `-v/--version`.

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `AIFINPAY_AGENT_SECRET` | — | base58 agent secret. Loads a stable identity; omit for a fresh ephemeral agent. |
| `AIFINPAY_BASE_URL` | `https://aifinpay.io` | API base URL (override for staging/local). |
| `AIFINPAY_TIMEOUT_MS` | `30000` | Per-call timeout in milliseconds. |
| `AIFINPAY_MAX_USD` | — | Hard cap (USD) per single payment. |
| `AIFINPAY_MCP_CMD` | `npx -y @aifinpay/mcp` | Override the MCP server launch command. |

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Success |
| `1` | Input error (missing/invalid flag) |
| `2` | Auth error (bad/missing secret) |
| `3` | Not found (e.g. unknown provider / not in registry) |
| `4` | Server error |
| `5` | Network error (MCP server failed to launch / connect) |

## Piping & composition

```bash
# Extract the EVM address only
aifinpay address | jq -r .evm

# Pipe a request body in from stdin
echo '{"messages":[{"role":"user","content":"hi"}]}' | aifinpay call --provider io-net --stdin

# Quote first, then decide whether to pay
aifinpay quote --url https://paid.example.com/data | jq '{status, facilitator}'

# Enforce a budget cap
aifinpay fetch --url https://paid.example.com/data --max-amount-usd 0.25

# Fee breakdown, grab just the total to transfer
aifinpay quote-split --chain solana --merchant-amount 5000000 | jq -r .total
```

## Notes

- **Stdout = data, stderr = everything else.** Safe to pipe straight into `jq`.
- **JSON by default.** `--output table` is for humans; scripts should rely on JSON.
- **No prompts, ever.** A missing required value exits with code `1` and a message on stderr.
- Payments settle on-chain (Polygon via `B2BSplitter`, Solana via Seat PDA). Fund
  the relevant address from `aifinpay address` before calling paid endpoints.

Built on [`@aifinpay/mcp`](https://www.npmjs.com/package/@aifinpay/mcp) · [aifinpay.io](https://aifinpay.io)

MIT © AiFinPay (AiFinPay)
