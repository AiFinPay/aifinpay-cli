#!/bin/sh
set -e

# aifinpay installer — downloads the right binary from GitHub Releases.
# Usage: curl -fsSL https://raw.githubusercontent.com/AiFinPay/aifinpay-cli/main/install.sh | sh

REPO="AiFinPay/aifinpay-cli"
NAME="aifinpay"

detect_os() {
  case "$(uname -s | tr '[:upper:]' '[:lower:]')" in
    linux*)  echo "linux" ;;
    darwin*) echo "darwin" ;;
    mingw*|msys*|cygwin*) echo "windows" ;;
    *) echo "unsupported" ;;
  esac
}
detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "amd64" ;;
    aarch64|arm64) echo "arm64" ;;
    *) echo "unsupported" ;;
  esac
}

OS=$(detect_os); ARCH=$(detect_arch)
if [ "$OS" = "unsupported" ] || [ "$ARCH" = "unsupported" ]; then
  echo "Error: unsupported platform $(uname -s)/$(uname -m)" >&2; exit 1
fi

EXT=""; [ "$OS" = "windows" ] && EXT=".exe"
ASSET="${NAME}-${OS}-${ARCH}${EXT}"
URL="https://github.com/${REPO}/releases/latest/download/${ASSET}"

# Pick an install dir on PATH that we can write to.
if [ -w "/usr/local/bin" ] 2>/dev/null; then DEST="/usr/local/bin"
elif [ -n "${HOME:-}" ]; then DEST="${HOME}/.local/bin"; mkdir -p "$DEST"
else DEST="."; fi

echo "Downloading ${ASSET} -> ${DEST}/${NAME}${EXT}"
TMP="$(mktemp)"
if command -v curl >/dev/null 2>&1; then curl -fsSL "$URL" -o "$TMP"
elif command -v wget >/dev/null 2>&1; then wget -qO "$TMP" "$URL"
else echo "Error: need curl or wget" >&2; exit 1; fi

chmod +x "$TMP"
mv "$TMP" "${DEST}/${NAME}${EXT}"

echo "Installed ${NAME} to ${DEST}/${NAME}${EXT}"
case ":${PATH}:" in
  *":${DEST}:"*) : ;;
  *) echo "Note: ${DEST} is not on your PATH — add it: export PATH=\"${DEST}:\$PATH\"" >&2 ;;
esac
echo "Requires Node.js on the host (the CLI launches @aifinpay/mcp via npx). Run: ${NAME} --help"
