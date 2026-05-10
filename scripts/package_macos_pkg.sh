#!/usr/bin/env bash
set -Eeuo pipefail

APP_NAME="relayx"
IDENTIFIER="org.lycheelab.relayx"
OUTPUT_DIR="dist"
VERSION=""
ARCH=""
SIGN_IDENTITY="${PKG_SIGN_IDENTITY:-}"

usage() {
  cat <<'EOF'
Build a macOS .pkg installer for relayx.

Usage:
  scripts/package_macos_pkg.sh [--version VERSION] [--arch amd64|arm64] [--output-dir DIR]

Environment overrides:
  PKG_SIGN_IDENTITY   Optional Developer ID Installer identity for productsign.

The package installs:
  /usr/local/bin/relayx
  /usr/local/share/relayx/README.md
  /usr/local/share/relayx/LICENSE
  /usr/local/share/relayx/relayx.env.example
  /usr/local/share/relayx/install-from-source.sh
  /usr/local/share/relayx/uninstall.sh
EOF
}

fail() {
  printf '[pkg] error: %s\n' "$*" >&2
  exit 1
}

log() {
  printf '[pkg] %s\n' "$*"
}

default_arch() {
  case "$(uname -m)" in
    x86_64) printf 'amd64\n' ;;
    arm64|aarch64) printf 'arm64\n' ;;
    *) fail "unsupported host architecture: $(uname -m)" ;;
  esac
}

default_version() {
  if command -v git >/dev/null 2>&1 && git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    git describe --tags --always --dirty
    return 0
  fi

  printf '0.0.0\n'
}

filename_version() {
  printf '%s' "$1" | tr -c 'A-Za-z0-9._-' '_'
}

pkg_receipt_version() {
  local raw="${1#v}"
  local numeric

  if [[ "$raw" =~ ^[0-9]+([.][0-9]+){0,3}$ ]]; then
    printf '%s\n' "$raw"
    return 0
  fi

  numeric="$(printf '%s' "$raw" | sed -E 's/^([0-9]+([.][0-9]+){0,3}).*/\1/')"
  if [[ "$numeric" =~ ^[0-9]+([.][0-9]+){0,3}$ ]]; then
    printf '%s\n' "$numeric"
    return 0
  fi

  printf '0.0.0\n'
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version)
      [ "$#" -ge 2 ] || fail "--version requires a value"
      VERSION="$2"
      shift 2
      ;;
    --arch)
      [ "$#" -ge 2 ] || fail "--arch requires a value"
      ARCH="$2"
      shift 2
      ;;
    --output-dir)
      [ "$#" -ge 2 ] || fail "--output-dir requires a value"
      OUTPUT_DIR="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      fail "unknown argument: $1"
      ;;
  esac
done

[ "$(uname -s)" = "Darwin" ] || fail "macOS is required because pkgbuild is an Apple tool"
command -v go >/dev/null 2>&1 || fail "go is required"
command -v pkgbuild >/dev/null 2>&1 || fail "pkgbuild is required"

if [ -n "$SIGN_IDENTITY" ]; then
  command -v productsign >/dev/null 2>&1 || fail "productsign is required when PKG_SIGN_IDENTITY is set"
fi

VERSION="${VERSION:-"$(default_version)"}"
ARCH="${ARCH:-"$(default_arch)"}"

case "$ARCH" in
  amd64|arm64) ;;
  *) fail "unsupported --arch: $ARCH" ;;
esac

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

VERSION_FILE="$(filename_version "$VERSION")"
PKG_VERSION="$(pkg_receipt_version "$VERSION")"
PACKAGE_NAME="${APP_NAME}-${VERSION_FILE}-darwin-${ARCH}"

TMP_DIR="$(mktemp -d)"
cleanup() {
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

PAYLOAD_DIR="$TMP_DIR/payload"
SCRIPTS_DIR="$TMP_DIR/scripts"
UNSIGNED_PKG="$TMP_DIR/${PACKAGE_NAME}.pkg"
FINAL_PKG="$OUTPUT_DIR/${PACKAGE_NAME}.pkg"

mkdir -p "$PAYLOAD_DIR/usr/local/bin" "$PAYLOAD_DIR/usr/local/share/$APP_NAME" "$SCRIPTS_DIR" "$OUTPUT_DIR"

log "building ${APP_NAME} for darwin/${ARCH}"
CGO_ENABLED=0 GOOS=darwin GOARCH="$ARCH" \
  go build -trimpath -ldflags="-s -w" -o "$PAYLOAD_DIR/usr/local/bin/$APP_NAME" ./cmd/relayx
chmod 0755 "$PAYLOAD_DIR/usr/local/bin/$APP_NAME"

cp README.md LICENSE "$PAYLOAD_DIR/usr/local/share/$APP_NAME/"
cp scripts/install.sh "$PAYLOAD_DIR/usr/local/share/$APP_NAME/install-from-source.sh"
chmod 0755 "$PAYLOAD_DIR/usr/local/share/$APP_NAME/install-from-source.sh"

cat >"$PAYLOAD_DIR/usr/local/share/$APP_NAME/uninstall.sh" <<EOF
#!/usr/bin/env bash
set -Eeuo pipefail

rm -f /usr/local/bin/$APP_NAME
rm -rf /usr/local/share/$APP_NAME
pkgutil --forget "$IDENTIFIER" >/dev/null 2>&1 || true

printf 'Uninstalled %s from /usr/local/bin\\n' "$APP_NAME"
EOF
chmod 0755 "$PAYLOAD_DIR/usr/local/share/$APP_NAME/uninstall.sh"

cat >"$PAYLOAD_DIR/usr/local/share/$APP_NAME/relayx.env.example" <<'EOF'
# relayx runtime configuration.
# Copy this file to a user-owned location before editing, for example:
#   cp /usr/local/share/relayx/relayx.env.example ~/.config/relayx/relayx.env

RELAYX_LISTEN_ADDR=127.0.0.1:8787
RELAYX_CODEX_MODE=disabled
RELAYX_CODEX_BIN=codex
RELAYX_DB=$HOME/.local/state/relayx/state.json
RELAYX_AUDIT_LOG=$HOME/.local/state/relayx/audit.jsonl

# Optional safety controls:
# RELAYX_AUTHORIZED_USERS=ou_xxx,ou_yyy
# RELAYX_ALLOWED_REPOS=/path/to/repo-a,/path/to/repo-b

# Feishu OpenAPI / callback settings:
# FEISHU_APP_ID=cli_xxx
# FEISHU_APP_SECRET=xxx
# FEISHU_VERIFICATION_TOKEN=xxx
EOF
chmod 0644 "$PAYLOAD_DIR/usr/local/share/$APP_NAME/relayx.env.example"

cat >"$SCRIPTS_DIR/postinstall" <<'EOF'
#!/bin/bash
set -e

chmod 0755 /usr/local/bin/relayx

cat <<'MSG'
relayx has been installed to /usr/local/bin/relayx.

Runtime config template:
  /usr/local/share/relayx/relayx.env.example

Uninstall:
  sudo /usr/local/share/relayx/uninstall.sh

Codex CLI is required when RELAYX_CODEX_MODE=app-server.
If codex is missing, install it before starting relayx.
MSG

exit 0
EOF
chmod 0755 "$SCRIPTS_DIR/postinstall"

if command -v xattr >/dev/null 2>&1; then
  xattr -cr "$PAYLOAD_DIR"
fi

log "building package ${PACKAGE_NAME}.pkg"
COPYFILE_DISABLE=1 pkgbuild \
  --root "$PAYLOAD_DIR" \
  --scripts "$SCRIPTS_DIR" \
  --identifier "$IDENTIFIER" \
  --version "$PKG_VERSION" \
  --install-location / \
  "$UNSIGNED_PKG" >/dev/null

if [ -n "$SIGN_IDENTITY" ]; then
  log "signing package with ${SIGN_IDENTITY}"
  productsign --sign "$SIGN_IDENTITY" "$UNSIGNED_PKG" "$FINAL_PKG" >/dev/null
else
  cp "$UNSIGNED_PKG" "$FINAL_PKG"
fi

log "wrote $FINAL_PKG"
