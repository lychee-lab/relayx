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
  RELAYX_HOME         RelayX config and runtime directory. Defaults to $HOME/.relayx.
  CONFIG_FILE         Config file path. Defaults to $RELAYX_HOME/config.toml.
  CODEX_INSTALL_CMD   Command used when codex is missing.

Default behavior:
  - If codex is not found, install it first.
  - On macOS with Homebrew, the default Codex install command is:
      brew install --cask codex
  - Build this Go app and install the binary to $BINDIR/relayx.
  - Write a TOML config template to $RELAYX_HOME/config.toml.
  - Create $RELAYX_HOME/run and $RELAYX_HOME/logs.
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
RELAYX_HOME="${RELAYX_HOME:-"$HOME/.relayx"}"
CONFIG_FILE="${CONFIG_FILE:-"$RELAYX_HOME/config.toml"}"
RUN_DIR="$RELAYX_HOME/run"
LOG_DIR="$RELAYX_HOME/logs"
STATE_FILE="$RELAYX_HOME/state.json"
AUDIT_FILE="$LOG_DIR/audit.jsonl"
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
  log "writing config template to $CONFIG_FILE"
  run mkdir -p "$(dirname "$CONFIG_FILE")" "$RUN_DIR" "$LOG_DIR"

  if [ "$DRY_RUN" -eq 1 ]; then
    printf '[dry-run] write %q\n' "$CONFIG_FILE"
    return 0
  fi

  if [ -f "$CONFIG_FILE" ]; then
    log "config exists; leaving unchanged"
    return 0
  fi

  cat >"$CONFIG_FILE" <<EOF
# relayx runtime configuration.

listen_addr = "$DEFAULT_LISTEN_ADDR"
codex_mode = "disabled"
codex_bin = "codex"
runtime_dir = "~/.relayx/run"
db = "~/.relayx/state.json"
audit_log = "~/.relayx/logs/audit.jsonl"

authorized_users = []
allowed_repos = []

[feishu]
app_id = ""
app_secret = ""
receive_mode = "long_connection"
base_url = "https://open.feishu.cn/open-apis"
verification_token = ""
EOF
  chmod 0600 "$CONFIG_FILE"
}

print_next_steps() {
  cat <<EOF

Installed $APP_NAME.

Binary:
  $INSTALL_BIN

Config:
  $CONFIG_FILE

Runtime directory:
  $RUN_DIR

Log directory:
  $LOG_DIR

Next steps:
  1. Ensure $BINDIR is on PATH.
  2. Edit $CONFIG_FILE with Feishu credentials and safety controls.
  3. Run:
       $INSTALL_BIN check
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
