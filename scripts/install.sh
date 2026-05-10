#!/usr/bin/env bash
set -Eeuo pipefail

APP_NAME="relayx"
DEFAULT_LISTEN_ADDR="127.0.0.1:8787"

DRY_RUN=0
SKIP_CODEX_INSTALL=0

usage() {
  cat <<'EOF'
Install relayx.

Usage:
  scripts/install.sh [--dry-run] [--skip-codex-install]

Environment overrides:
  PREFIX              Install prefix. Defaults to $HOME/.local.
  BINDIR              Binary install directory. Defaults to $PREFIX/bin.
  CONFIG_DIR          Config directory. Defaults to $HOME/.config/relayx.
  STATE_DIR           State directory. Defaults to $HOME/.local/state/relayx.
  CODEX_INSTALL_CMD   Command used when codex is missing.

Default behavior:
  - If codex is not found, install it first.
  - On macOS with Homebrew, the default Codex install command is:
      brew install --cask codex
  - Build this Go app and install the binary to $BINDIR/relayx.
  - Write an env template to $CONFIG_DIR/relayx.env.
EOF
}

log() {
  printf '[install] %s\n' "$*"
}

fail() {
  printf '[install] error: %s\n' "$*" >&2
  exit 1
}

run() {
  if [ "$DRY_RUN" -eq 1 ]; then
    printf '[dry-run]'
    printf ' %q' "$@"
    printf '\n'
    return 0
  fi
  "$@"
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    --skip-codex-install)
      SKIP_CODEX_INSTALL=1
      shift
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

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

PREFIX="${PREFIX:-"$HOME/.local"}"
BINDIR="${BINDIR:-"$PREFIX/bin"}"
CONFIG_DIR="${CONFIG_DIR:-"$HOME/.config/$APP_NAME"}"
STATE_DIR="${STATE_DIR:-"$HOME/.local/state/$APP_NAME"}"
STATE_FILE="$STATE_DIR/state.json"
AUDIT_FILE="$STATE_DIR/audit.jsonl"
ENV_FILE="$CONFIG_DIR/$APP_NAME.env"
BUILD_DIR="$REPO_ROOT/.build"
BUILD_BIN="$BUILD_DIR/$APP_NAME"
INSTALL_BIN="$BINDIR/$APP_NAME"

detect_codex_install_cmd() {
  if [ -n "${CODEX_INSTALL_CMD:-}" ]; then
    printf '%s\n' "$CODEX_INSTALL_CMD"
    return 0
  fi

  case "$(uname -s)" in
    Darwin)
      if command -v brew >/dev/null 2>&1; then
        printf '%s\n' "brew install --cask codex"
        return 0
      fi
      ;;
  esac

  return 1
}

install_codex_if_missing() {
  if command -v codex >/dev/null 2>&1; then
    log "codex found: $(command -v codex)"
    return 0
  fi

  if [ "$SKIP_CODEX_INSTALL" -eq 1 ]; then
    fail "codex is not installed and --skip-codex-install was set"
  fi

  local cmd
  if ! cmd="$(detect_codex_install_cmd)"; then
    fail "codex is not installed and no supported installer was found. Install Codex CLI first, or set CODEX_INSTALL_CMD."
  fi

  log "codex not found; installing with: $cmd"
  if [ "$DRY_RUN" -eq 1 ]; then
    printf '[dry-run] %s\n' "$cmd"
  else
    # shellcheck disable=SC2086
    sh -c "$cmd"
  fi

  if [ "$DRY_RUN" -eq 0 ] && ! command -v codex >/dev/null 2>&1; then
    fail "codex install command completed but codex is still not on PATH"
  fi
}

require_build_tools() {
  command -v go >/dev/null 2>&1 || fail "go is required to build $APP_NAME"
  command -v install >/dev/null 2>&1 || fail "install is required"
}

build_app() {
  log "building $APP_NAME"
  run mkdir -p "$BUILD_DIR"
  run go build -trimpath -o "$BUILD_BIN" ./cmd/relayx
}

install_app() {
  log "installing binary to $INSTALL_BIN"
  run mkdir -p "$BINDIR"
  run install -m 0755 "$BUILD_BIN" "$INSTALL_BIN"
}

write_default_config() {
  log "writing config template to $ENV_FILE"
  run mkdir -p "$CONFIG_DIR" "$STATE_DIR"

  if [ "$DRY_RUN" -eq 1 ]; then
    printf '[dry-run] write %q\n' "$ENV_FILE"
    return 0
  fi

  if [ -f "$ENV_FILE" ]; then
    log "config exists; leaving unchanged"
    return 0
  fi

  cat >"$ENV_FILE" <<EOF
# relayx runtime configuration.
# Fill Feishu values before enabling Feishu callbacks in production.

RELAYX_LISTEN_ADDR=$DEFAULT_LISTEN_ADDR
RELAYX_CODEX_MODE=disabled
RELAYX_CODEX_BIN=codex
RELAYX_DB=$STATE_FILE
RELAYX_AUDIT_LOG=$AUDIT_FILE

# Optional safety controls:
# RELAYX_AUTHORIZED_USERS=ou_xxx,ou_yyy
# RELAYX_ALLOWED_REPOS=/path/to/repo-a,/path/to/repo-b

# Feishu OpenAPI / callback settings:
# FEISHU_APP_ID=cli_xxx
# FEISHU_APP_SECRET=xxx
# FEISHU_VERIFICATION_TOKEN=xxx
EOF
  chmod 0600 "$ENV_FILE"
}

print_next_steps() {
  cat <<EOF

Installed $APP_NAME.

Binary:
  $INSTALL_BIN

Config template:
  $ENV_FILE

State directory:
  $STATE_DIR

Next steps:
  1. Ensure $BINDIR is on PATH.
  2. Edit $ENV_FILE with Feishu credentials and safety controls.
  3. Run:
       set -a
       . "$ENV_FILE"
       set +a
       $INSTALL_BIN serve
EOF
}

main() {
  cd "$REPO_ROOT"
  install_codex_if_missing
  require_build_tools
  build_app
  install_app
  write_default_config
  print_next_steps
}

main "$@"

