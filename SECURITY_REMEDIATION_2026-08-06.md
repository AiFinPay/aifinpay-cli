# AiFinPay CLI security remediation — 2026-08-06 audit

Status: **IN PROGRESS / RELEASE BLOCKED**

This branch remediates CLI findings from the AiFinPay x402 SDK/MCP/NPM audit dated 2026-08-06.

## Implemented and regression-tested

- Encrypted persistent keystore: AES-256-GCM authenticated encryption.
- PBKDF2-HMAC-SHA256 password KDF with random salt and 600,000 iterations.
- Atomic keystore writes; file mode repaired to 0600 and directory to 0700.
- Legacy plaintext keystore migrates one-way to encrypted v2 when a passphrase is supplied.
- `aifinpay import` refuses private keys in argv and reads the secret from stdin.
- Installer verifies release binaries against `SHA256SUMS` before installation.
- Default MCP child command is pinned to an exact package version and npm lifecycle scripts are disabled.
- Custom `AIFINPAY_MCP_CMD` requires explicit `AIFINPAY_ALLOW_CUSTOM_MCP=1` opt-in.
- GitHub Actions gates gofmt, go vet, race-enabled unit tests, build, installer syntax and checksum enforcement.

## Still release-blocking

- The CLI still transports `AIFINPAY_AGENT_SECRET` to the MCP child through the child environment. Exact package pinning is containment, not a complete secret-transport redesign.
- The keystore passphrase must be filtered from the child environment before release.
- The exact MCP package pin must be updated to the final independently audited MCP release, not a pre-release placeholder.
- Final clean-machine install test and independent audit are required.

Do not merge or publish this branch until these blockers are closed and the SDK/MCP remediation release has passed its own final audit.
