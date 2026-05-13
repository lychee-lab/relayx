#!/usr/bin/env bash
set -Eeuo pipefail

APP_NAME="relayx"
PREFIX="${PREFIX:-"$HOME/.local"}"
BINDIR="${BINDIR:-"$PREFIX/bin"}"
RELAYX_HOME="${RELAYX_HOME:-"$HOME/.relayx"}"
CONFIG_FILE="${CONFIG_FILE:-"$RELAYX_HOME/config.tomL"}"
RUN_DIR="$RELAYX_HOME/run"
LOG_DIR="$RELAYX_HOME/logs"
STATE_FILE="$RELAYX_HOME/state.json"

REMOVE_CONFIG=0
REMOVE_STATE=0

usage() {
  cat <<'EOF'
Uninstall relayx.

Usage:
  scripts/uninstall.sh [--remove-config] [--remove-state]

Environment overrides:
  PREFIX
  BINDIR
  RELAYX_HOME
  CONFIG_FILE
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --remove-config)
      REMOVE_CONFIG=1
      shift
      ;;
    --remove-state)
      REMOVE_STATE=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      printf 'unknown argument: %s\n' "$1" >&2
      exit 1
      ;;
  esac
done

rm -f "$BINDIR/$APP_NAME"

if [ "$REMOVE_CONFIG" -eq 1 ]; then
  rm -f "$CONFIG_FILE"
fi

if [ "$REMOVE_STATE" -eq 1 ]; then
  rm -f "$STATE_FILE"
  rm -rf "$RUN_DIR" "$LOG_DIR"
fi

rmdir "$RELAYX_HOME" 2>/dev/null || true

printf 'Uninstalled %s from %s\n' "$APP_NAME" "$BINDIR"
