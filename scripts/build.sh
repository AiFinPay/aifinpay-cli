#!/usr/bin/env bash
# Cross-compile aifinpay for all supported platforms into dist/ + checksums.
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="${1:-$(go run . --version 2>/dev/null | awk '{print $2}')}"
OUT="dist"
rm -rf "$OUT"; mkdir -p "$OUT"

PLATFORMS="linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64"

for p in $PLATFORMS; do
  GOOS="${p%/*}"; GOARCH="${p#*/}"
  ext=""; [ "$GOOS" = "windows" ] && ext=".exe"
  bin="aifinpay-${GOOS}-${GOARCH}${ext}"
  echo "→ $bin"
  CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" \
    go build -trimpath -ldflags "-s -w" -o "$OUT/$bin" .
done

echo "→ SHA256SUMS"
( cd "$OUT" && (sha256sum aifinpay-* 2>/dev/null || shasum -a 256 aifinpay-*) > SHA256SUMS )

echo "Done. Artifacts in $OUT/:"
ls -lh "$OUT"
