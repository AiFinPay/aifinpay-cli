# CLI security release gate

The CLI release is blocked unless all conditions below are true:

1. `go test -race ./...`, `go vet ./...`, build and installer checks pass in CI.
2. No private key is accepted from command-line arguments.
3. Persistent private-key storage is authenticated ciphertext only.
4. Keystore directory/file permissions are 0700/0600 after both create and overwrite.
5. Release installer rejects a binary whose SHA-256 does not match `SHA256SUMS`.
6. The child MCP command is pinned to the independently audited MCP release.
7. Keystore passphrase is not inherited by the MCP child.
8. Any custom MCP child requires explicit operator opt-in and is visibly logged.
9. A clean-machine installation test passes from the actual release artifacts.
10. Final independent audit finds no unresolved critical/high key-exposure path.
