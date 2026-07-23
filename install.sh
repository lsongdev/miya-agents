#!/bin/sh
set -eu

REPO="lsongdev/miya-agents"
BINARY="miya"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"

# --- OS detection ---
detect_os() {
  os=$(uname -s)
  case "$os" in
    Linux*)  echo "linux"  ;;
    Darwin*) echo "darwin" ;;
    *)       echo "unsupported" ;;
  esac
}

# --- Arch detection ---
detect_arch() {
  arch=$(uname -m)
  case "$arch" in
    x86_64|amd64)  echo "amd64" ;;
    aarch64|arm64) echo "arm64" ;;
    *)             echo "unsupported" ;;
  esac
}

OS=$(detect_os)
ARCH=$(detect_arch)
if [ "$OS" = "unsupported" ] || [ "$ARCH" = "unsupported" ]; then
  echo "Unsupported platform: ${OS}/${ARCH}" >&2
  exit 1
fi

# --- Resolve version ---
VERSION="${1:-}"
if [ -z "$VERSION" ]; then
  AUTH_HEADER=""
  if [ -n "${GITHUB_TOKEN:-}" ]; then
    AUTH_HEADER="-H \"Authorization: token ${GITHUB_TOKEN}\""
  fi
  VERSION=$(eval curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" "$AUTH_HEADER" \
    | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
  if [ -z "$VERSION" ]; then
    echo "Failed to resolve latest version" >&2
    exit 1
  fi
fi

# --- Download ---
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

ASSET="${BINARY}-${OS}-${ARCH}"
if [ "$OS" = "windows" ]; then
  ASSET="${ASSET}.exe"
fi

URL="https://github.com/${REPO}/releases/download/${VERSION}/${ASSET}"
echo "Downloading ${URL} ..."
curl -fsSL "$URL" -o "${TMPDIR}/${ASSET}"

# --- Install ---
mkdir -p "$INSTALL_DIR"
install -m 755 "${TMPDIR}/${ASSET}" "${INSTALL_DIR}/${BINARY}"

# macOS: clear quarantine
if [ "$OS" = "darwin" ]; then
  xattr -cr "${INSTALL_DIR}/${BINARY}" 2>/dev/null || true
fi

echo "Installed ${BINARY} ${VERSION} to ${INSTALL_DIR}/${BINARY}"

# --- PATH hint ---
case ":${PATH}:" in
  *":${INSTALL_DIR}:"*) ;;
  *)
    echo ""
    echo "Add to your PATH:"
    echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
    ;;
esac
