# aifinpay — CLI for AiFinPay

Agent-first command-line interface for AiFinPay. It wraps
[`@aifinpay/mcp`](https://www.npmjs.com/package/@aifinpay/mcp) and keeps stdout
machine-readable.

## Canonical economics

| Route | Payer | Merchant/provider | AiFinPay | Creator |
|---|---:|---:|---:|---:|
| AIFP-1 | exact quoted gross | 99% | 1% | 0% |
| AIFP-2 / x402 | provider price | 100% | 0% | 0% |

Network gas is separate. AIFP-1 is gross-inclusive; no fee is added on top.
AIFP-2/x402 has a temporary 0% AiFinPay protocol fee for the current launch
period.

The historical `pay-split` and `quote-split` commands are retained only as
compatibility names. They exit with an input error and never create an invoice
or move funds. Use a canonical AIFP-1 quote/settlement client with an
independently pinned v1.3 deployment and runtime profile.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/AiFinPay/aifinpay-cli/main/install.sh | sh
```

Or build from source with Go 1.21+:

```bash
go install github.com/AiFinPay/aifinpay-cli@latest
```

## Commands

| Command | Status |
|---|---|
| `aifinpay address` | Show locally derived agent addresses; no funds move. |
| `aifinpay quote` | Inspect an HTTP 402 challenge; no funds move. |
| `aifinpay fetch` | Pay only when the MCP/SDK trust and value gates pass. |
| `aifinpay call` | Registry-resolved provider call with the same fail-closed gates. |
| `aifinpay pay-split` | Retired; exits without creating an invoice. |
| `aifinpay quote-split` | Retired; use a canonical AIFP-1 quote. |

## Required safety configuration

`AIFINPAY_MAX_USD` must be a positive finite operator ceiling for any
fund-moving call. Tool input may tighten but cannot increase it. Unknown asset
price, untrusted target, missing runtime hash/profile, expired quote or economic
mismatch must block signing.

| Variable | Default | Purpose |
|---|---|---|
| `AIFINPAY_AGENT_SECRET` | — | Persistent local agent secret. Treat as a wallet credential. |
| `AIFINPAY_BASE_URL` | `https://aifinpay.io` | Trusted API origin. |
| `AIFINPAY_TIMEOUT_MS` | `30000` | Per-call timeout. |
| `AIFINPAY_MAX_USD` | — | Mandatory operator ceiling for fund-moving tools. |
| `AIFINPAY_MCP_CMD` | `npx -y @aifinpay/mcp` | MCP server launcher override. |

## Production boundary

A documented network or historical transaction is not proof of current payment
support. The exact deployment, chain ID, runtime hash, BPS profile, target,
asset and paid E2E evidence must match the reviewed release. Otherwise the CLI
must remain fail-closed.

MIT © AiFinPay
