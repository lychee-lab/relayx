# relayx

`relayx` is a local-first control plane for driving Codex CLI from Feishu.

The service will run beside Codex CLI, expose key Codex events to Feishu, and accept
approved user actions from Feishu cards or bot messages.

Current status: functional local service with Codex JSON-RPC, Feishu HTTP callback
handling, file-backed state, audit logging, and test coverage.

## Language Choice

The project uses Go.

Go is the best fit among Python, Go, and Rust for this service because it gives us
a small single binary, straightforward process supervision, mature concurrency
primitives, and an official Feishu SDK path. Python is faster for scripting but
less robust for long-running local daemons. Rust is strong for safety, but the
Feishu integration ecosystem and iteration cost are worse for this product shape.

## Quick Start

```bash
go test ./...
go test -race ./...
go vet ./...
go run ./cmd/relayx check
go run ./cmd/relayx parse "/codex start repo=/tmp/demo fix the failing test"
go run ./cmd/relayx serve
```

## Install

```bash
scripts/install.sh
```

The installer checks for `codex` first. If Codex CLI is missing on macOS and
Homebrew is available, it runs `brew install --cask codex` before installing this
app.

Default install locations:

```text
Binary:  $HOME/.local/bin/relayx
Config:  $HOME/.config/relayx/relayx.env
State:   $HOME/.local/state/relayx
```

Useful overrides:

```bash
BINDIR=/usr/local/bin scripts/install.sh
PREFIX=/opt/relayx scripts/install.sh
CODEX_INSTALL_CMD='brew install --cask codex' scripts/install.sh
scripts/install.sh --dry-run
```

Development message endpoint:

```bash
curl -sS http://127.0.0.1:8787/dev/message \
  -H 'content-type: application/json' \
  -d '{"chat_id":"oc_demo","user_id":"ou_demo","text":"/codex start repo=/tmp/demo fix the failing test"}'
```

## Runtime Modes

Default mode is safe local development mode. It does not start Codex.

To start and drive Codex app-server through stdio JSON-RPC:

```bash
RELAYX_CODEX_MODE=app-server go run ./cmd/relayx serve
```

Feishu HTTP callback endpoint:

```text
POST /feishu/events
```

Feishu OpenAPI sending is enabled when these variables are present:

```bash
FEISHU_APP_ID=cli_xxx
FEISHU_APP_SECRET=xxx
FEISHU_VERIFICATION_TOKEN=xxx
```

Recommended safety controls:

```bash
RELAYX_AUTHORIZED_USERS=ou_xxx,ou_yyy
RELAYX_ALLOWED_REPOS=/Users/me/project-a,/Users/me/project-b
RELAYX_DB=.relayx/state.json
RELAYX_AUDIT_LOG=.relayx/audit.jsonl
```

Legacy `CODEX_BABYSITTER_*` variables are still accepted as fallback while migrating
existing local configs to `RELAYX_*`.

## Docs

- [Technical Design](docs/technical-design.md)
- [Milestone 01 Plan](docs/milestone-01-control-core.md)

## Implemented Components

- Command parser for `/codex start/status/steer/stop/diff/logs/help`.
- In-memory task and approval state manager.
- Policy checks for authorized users and allowed repo roots.
- Secret redaction for outbound status and approval summaries.
- File-backed state snapshot and JSONL audit log.
- Codex app-server stdio JSON-RPC adapter.
- Feishu OpenAPI message/card sender.
- Feishu HTTP callback receiver for URL verification, bot messages, and card actions.
- Shared HTTP API for local dev and Feishu callbacks.
- Unit tests, race tests, and an in-process end-to-end test.
