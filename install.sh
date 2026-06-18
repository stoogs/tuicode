#!/usr/bin/env bash
# tuicode installer — builds the binary and installs it to a bin dir on PATH.
# Usage: ./install.sh [--prefix DIR]
#
# tuicode itself does NOT install OpenCode or Ollama. Those are separate
# prerequisites; tuicode's first-run check will guide you if either is missing.
set -euo pipefail

PREFIX="${HOME}/.local/bin"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --prefix) PREFIX="$2"; shift 2 ;;
    --prefix=*) PREFIX="${1#*=}"; shift ;;
    -h|--help)
      echo "Usage: ./install.sh [--prefix DIR]   (default: ~/.local/bin)"
      exit 0 ;;
    *) echo "unknown option: $1" >&2; exit 1 ;;
  esac
done

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$here"

if ! command -v go >/dev/null 2>&1; then
  echo "error: Go toolchain not found. Install Go 1.23+ first:" >&2
  echo "  macOS:  brew install go" >&2
  echo "  Arch:   sudo pacman -S go" >&2
  echo "  Fedora: sudo dnf install golang" >&2
  echo "  Ubuntu: sudo apt install golang" >&2
  exit 1
fi

echo "==> Building tuicode…"
VERSION="$(git describe --tags --always 2>/dev/null || echo 0.1.0)"
mkdir -p "$PREFIX"
go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o "${PREFIX}/tuicode" .

echo "==> Installed: ${PREFIX}/tuicode  (version ${VERSION})"

case ":${PATH}:" in
  *":${PREFIX}:"*) ;;
  *) echo "note: ${PREFIX} is not on your PATH. Add this to your shell rc:"
     echo "      export PATH=\"${PREFIX}:\$PATH\"" ;;
esac

echo
echo "Prerequisites (install separately if the first-run check flags them):"
echo "  - OpenCode  (the client tuicode launches)"
echo "  - Ollama    (the model backend; start its daemon)"
echo
echo "Run 'tuicode' to start."
