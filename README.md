# RelayX

RelayX is a local-first control plane for driving Codex CLI from Feishu.

It runs next to Codex on your development machine, forwards important Codex
events to Feishu, and lets you approve permissions or send follow-up instructions
from a phone. The goal is to keep the actual code, shell, credentials, and
repository access local while using Feishu as a remote command and approval
surface.

## Why

Codex CLI is most useful when it can inspect a repo, run tests, edit files, and
ask for permission before risky operations. That workflow usually assumes you are
at the terminal.

RelayX adds a small local service around Codex so you can:

- Start a Codex task from Feishu.
- See task status and important Codex events in Feishu.
- Approve or reject command/file/permission requests from Feishu cards.
- Send additional instructions while a task is running.
- Keep Codex, repositories, and secrets on the local machine.

## Current Status

RelayX currently includes:

- Go CLI binary: `relayx`.
- Local HTTP API for development and Feishu callbacks.
- Codex app-server stdio JSON-RPC adapter.
- Feishu OpenAPI client for text messages and interactive approval cards.
- Feishu HTTP callback handler for URL verification, bot messages, and card actions.
- `/codex` command parser.
- In-memory task and approval state manager.
- File-backed state snapshot.
- JSONL audit log.
- User and repo allowlist policy.
- Secret redaction for outbound messages.
- Unit tests, race tests, and an in-process end-to-end test.

The Feishu receive path is currently implemented through HTTP callbacks. A Feishu
long-connection receiver can be added later as another adapter over the same app
service.

## Architecture

```text
Feishu mobile/client
  -> Feishu bot message or card action
  -> RelayX HTTP callback / local dev endpoint
  -> command router and approval router
  -> task manager, policy checks, audit log
  -> Codex app-server JSON-RPC adapter
  -> Codex CLI on the local machine
  -> local repository, shell, sandbox
```

Runtime processes:

```text
relayx
  - Feishu callback handler
  - Feishu OpenAPI sender
  - Codex JSON-RPC client
  - task and approval manager
  - state snapshot writer
  - audit log writer

codex app-server
  - launched by relayx when RELAYX_CODEX_MODE=app-server
  - connected through stdio JSON-RPC
```

RelayX does not expose Codex app-server directly to the public internet. Feishu
only talks to RelayX, and RelayX talks to Codex locally.

## Requirements

- Go 1.20 or newer.
- Codex CLI.
- Feishu app credentials if you want Feishu integration.
- A reachable HTTP callback URL for Feishu callback mode, such as a public relay,
  tunnel, or reverse proxy pointing to RelayX.

On macOS, the installer can install Codex CLI through Homebrew if `codex` is not
already available.

## Install

From the repo root:

```bash
scripts/install.sh
```

The installer:

- Checks whether `codex` is available.
- Installs Codex CLI first on macOS when Homebrew is present and Codex is missing.
- Builds `relayx`.
- Installs the binary to the default user program directory.
- Writes a config template.
- Creates the default state directory.

Default locations:

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

Uninstall:

```bash
scripts/uninstall.sh
scripts/uninstall.sh --remove-config --remove-state
```

## Quick Start

Run locally without starting Codex:

```bash
go run ./cmd/relayx check
go run ./cmd/relayx parse "/codex start repo=/tmp/demo fix the failing test"
go run ./cmd/relayx serve
```

In another terminal, simulate a Feishu message:

```bash
curl -sS http://127.0.0.1:8787/dev/message \
  -H 'content-type: application/json' \
  -d '{"chat_id":"oc_demo","user_id":"ou_demo","text":"/codex start repo=/tmp/demo fix the failing test"}'
```

Start RelayX with Codex app-server enabled:

```bash
RELAYX_CODEX_MODE=app-server go run ./cmd/relayx serve
```

## Configuration

RelayX is configured through environment variables.

Core settings:

```bash
RELAYX_LISTEN_ADDR=127.0.0.1:8787
RELAYX_CODEX_MODE=disabled
RELAYX_CODEX_BIN=codex
RELAYX_RUNTIME_DIR=.relayx/run
RELAYX_DB=.relayx/state.json
RELAYX_AUDIT_LOG=.relayx/audit.jsonl
```

Codex mode:

```text
disabled    Do not start Codex. Useful for local HTTP/dev testing.
app-server  Start `codex app-server --listen stdio://` and drive it via JSON-RPC.
```

Safety controls:

```bash
RELAYX_AUTHORIZED_USERS=ou_xxx,ou_yyy
RELAYX_ALLOWED_REPOS=/Users/me/project-a,/Users/me/project-b
```

Feishu settings:

```bash
FEISHU_APP_ID=cli_xxx
FEISHU_APP_SECRET=xxx
FEISHU_VERIFICATION_TOKEN=xxx
FEISHU_BASE_URL=https://open.feishu.cn/open-apis
```

Legacy `CODEX_BABYSITTER_*` environment variables are still accepted as fallback
while migrating existing local configs to `RELAYX_*`.

## Feishu Setup

Create a Feishu internal app and enable bot capabilities.

Configure event/callback subscriptions:

```text
im.message.receive_v1
card.action.trigger
```

RelayX callback endpoint:

```text
POST /feishu/events
```

Callback URL verification is handled by RelayX. Configure
`FEISHU_VERIFICATION_TOKEN` to match the token configured in Feishu.

Message flow:

```text
User sends /codex start ...
  -> Feishu sends im.message.receive_v1
  -> RelayX parses command and starts or updates a task
  -> Codex emits events or approval requests
  -> RelayX sends Feishu text messages or interactive approval cards
  -> User clicks approve/deny
  -> Feishu sends card.action.trigger
  -> RelayX responds to Codex
```

To test only credentials, RelayX needs `FEISHU_APP_ID` and
`FEISHU_APP_SECRET`. To test sending a message, RelayX also needs a target
`chat_id`, which is normally obtained from an incoming Feishu message event.

## Supported Feishu Commands

RelayX currently parses these bot commands:

```text
/codex start repo=/path/to/repo task description
/codex status
/codex steer additional instruction
/codex stop
/codex diff
/codex logs
/codex help
```

Current behavior:

- `start` creates a task and, in `app-server` mode, starts a Codex thread and turn.
- `status` returns the latest task status in the chat.
- `steer` records an additional instruction and, when possible, forwards it to Codex.
- `stop` marks the latest task as stopped.
- `diff` and `logs` are reserved command surfaces and currently return placeholder responses.

## Codex Integration

RelayX uses Codex app-server over stdio JSON-RPC.

Implemented Codex operations:

- `initialize`
- `thread/start`
- `turn/start`
- `turn/steer`
- command execution approval response
- file change approval response
- legacy exec approval response
- basic event handling for task state updates

RelayX maps Codex approval requests into Feishu cards. Approval decisions are
mapped back into the Codex protocol:

```text
approved              -> accept / approved
approved_for_session  -> acceptForSession / approved_for_session
denied                -> decline / denied
abort                 -> cancel / abort
```

## Security Model

RelayX is designed to be conservative by default.

Default safety properties:

- Codex stays local.
- RelayX defaults to `RELAYX_CODEX_MODE=disabled`.
- Codex app-server is driven through stdio when enabled.
- User allowlist can restrict who can control RelayX.
- Repo allowlist can restrict which paths can be used.
- Approval cards include only summaries.
- Outbound messages are passed through secret redaction.
- State is local.
- Audit log is local JSONL.

High-risk commands are identified by the policy layer. Current risk detection
covers patterns such as:

```text
rm -rf
git reset --hard
git clean -fd
sudo
chmod -R
curl | sh
wget | sh
danger-full-access
```

Secret redaction covers common token and key patterns before text is sent to
Feishu.

## State And Audit Files

RelayX writes local state and audit logs:

```text
RELAYX_DB         JSON snapshot of tasks and approvals.
RELAYX_AUDIT_LOG  JSONL audit trail of user actions and approval decisions.
```

The current persistence implementation is file-backed. The code keeps persistence
behind `core.Snapshot` and `persist.FileStateStore`, so a future SQLite-backed
store can be added without changing the app service boundary.

## Development

Run all tests:

```bash
go test ./...
go test -race ./...
go vet ./...
```

Run a specific package:

```bash
go test ./internal/codex
go test ./internal/e2e
```

Check install scripts:

```bash
bash -n scripts/install.sh
bash -n scripts/uninstall.sh
scripts/install.sh --dry-run
```

## Repository Layout

```text
cmd/relayx/          CLI entrypoint.
internal/app/        Service orchestration and task/approval handling.
internal/codex/      Codex app-server JSON-RPC adapter.
internal/config/     Environment config loading.
internal/core/       Commands, policy, redaction, task state.
internal/e2e/        In-process end-to-end tests.
internal/feishu/     Feishu OpenAPI client and callback handler.
internal/httpapi/    HTTP routes shared by dev and Feishu callback mode.
internal/persist/    File-backed state and audit log.
scripts/             Install and uninstall scripts.
docs/                Design notes and milestone plan.
```

## Roadmap

Near-term:

- Add a Feishu long-connection receiver adapter.
- Add richer `/codex diff` and `/codex logs` responses.
- Improve Codex event summarization.
- Add launchd/systemd service templates.
- Add optional SQLite persistence.

Later:

- Multiple concurrent task routing per chat.
- More granular approval policies.
- Web dashboard for local inspection.
- Cloud relay mode for users who do not want to expose an HTTP callback directly.

## Troubleshooting

Check whether RelayX sees Feishu configuration:

```bash
relayx check
```

Test local command parsing:

```bash
relayx parse "/codex start repo=/tmp/demo fix bug"
```

If Codex cannot start:

- Confirm `codex` is on `PATH`.
- Run `codex --version`.
- Run RelayX with `RELAYX_CODEX_MODE=disabled` to isolate Feishu/local HTTP issues.

If Feishu callbacks do not arrive:

- Confirm Feishu event subscriptions include `im.message.receive_v1` and `card.action.trigger`.
- Confirm callback URL points to `/feishu/events`.
- Confirm `FEISHU_VERIFICATION_TOKEN` matches Feishu app settings.
- Confirm your tunnel or reverse proxy can reach `RELAYX_LISTEN_ADDR`.

If Feishu sending fails:

- Confirm `FEISHU_APP_ID` and `FEISHU_APP_SECRET`.
- Confirm the bot is installed in the target tenant/chat.
- Confirm RelayX has a valid `chat_id` from an incoming event.

## License

See [LICENSE](LICENSE).
