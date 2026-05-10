#!/usr/bin/env bash
set -Eeuo pipefail

APP_NAME="relayx"
PREFIX="${PREFIX:-"$HOME/.local"}"
BINDIR="${BINDIR:-"$PREFIX/bin"}"
CONFIG_DIR="${CONFIG_DIR:-"$HOME/.config/$APP_NAME"}"
STATE_DIR="${STATE_DIR:-"$HOME/.local/state/$APP_NAME"}"

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
  CONFIG_DIR
  STATE_DIR
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
  rm -rf "$CONFIG_DIR"
fi

if [ "$REMOVE_STATE" -eq 1 ]; then
  rm -rf "$STATE_DIR"
fi

printf 'Uninstalled %s from %s\n' "$APP_NAME" "$BINDIR"

