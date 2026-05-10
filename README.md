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
- Approve or reject command, file, and permission requests from Feishu cards.
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

The high-level flow is:

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

macOS installer package:

```bash
scripts/package_macos_pkg.sh --version dev --arch arm64 --output-dir dist
sudo installer -pkg dist/relayx-dev-darwin-arm64.pkg -target /
```

The `.pkg` installs `relayx` to `/usr/local/bin/relayx` and places a config
template at `/usr/local/share/relayx/relayx.env.example`. Remove the package
with `sudo /usr/local/share/relayx/uninstall.sh`. GitHub Actions builds macOS
`.pkg` artifacts for both `amd64` and `arm64`; pushing a `v*` tag also attaches
them to the GitHub Release. Unsigned packages are useful for internal testing.
Public double-click distribution should use a Developer ID Installer certificate
and Apple notarization.

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
bash -n scripts/package_macos_pkg.sh
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

---

# RelayX 中文说明

RelayX 是一个本地优先的控制平面，用来通过飞书远程驱动 Codex CLI。

它运行在你的开发机上、位于 Codex 旁边，将重要的 Codex 事件转发到飞书，并允许你在手机上批准权限或发送追加指令。目标是让代码、Shell、凭证和仓库访问都保留在本机，同时把飞书作为远程指令和审批入口。

## 为什么需要 RelayX

当 Codex CLI 能够检查仓库、运行测试、编辑文件，并在高风险操作前请求权限时，它的价值最大。但这套工作流通常默认你就在电脑和终端旁边。

RelayX 在 Codex 外围增加一个小型本地服务，让你可以：

- 从飞书启动 Codex 任务。
- 在飞书里查看任务状态和重要 Codex 事件。
- 通过飞书卡片批准或拒绝命令、文件和权限请求。
- 在任务运行过程中发送追加指令。
- 让 Codex、仓库和敏感信息继续保留在本机。

## 当前状态

RelayX 当前已经包含：

- Go CLI 二进制：`relayx`。
- 用于开发调试和飞书回调的本地 HTTP API。
- Codex app-server 的 stdio JSON-RPC 适配器。
- 用于文本消息和交互式审批卡片的飞书 OpenAPI 客户端。
- 支持 URL verification、机器人消息和卡片操作的飞书 HTTP 回调处理器。
- `/codex` 指令解析器。
- 内存中的任务和审批状态管理器。
- 文件型状态快照。
- JSONL 审计日志。
- 用户和仓库白名单策略。
- 外发消息的敏感信息脱敏。
- 单元测试、race 测试和进程内端到端测试。

飞书接收链路当前通过 HTTP callback 实现。后续可以在同一套 app service 之上增加飞书长连接接收器，作为另一个输入适配器。

## 架构

整体链路如下：

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

运行时进程：

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

RelayX 不会把 Codex app-server 直接暴露到公网。飞书只和 RelayX 通信，RelayX 再在本机和 Codex 通信。

## 依赖要求

- Go 1.20 或更高版本。
- Codex CLI。
- 如果需要飞书集成，需要飞书应用凭证。
- 使用飞书 HTTP callback 模式时，需要一个可被飞书访问的回调 URL，例如公网 relay、内网穿透或指向 RelayX 的反向代理。

在 macOS 上，如果本机还没有 `codex`，安装脚本可以通过 Homebrew 安装 Codex CLI。

## 安装

在仓库根目录执行：

```bash
scripts/install.sh
```

安装脚本会：

- 检查 `codex` 是否可用。
- 在 macOS 且存在 Homebrew、但缺少 Codex 时，先安装 Codex CLI。
- 构建 `relayx`。
- 将二进制安装到默认用户程序目录。
- 写入配置模板。
- 创建默认状态目录。

默认位置：

```text
Binary:  $HOME/.local/bin/relayx
Config:  $HOME/.config/relayx/relayx.env
State:   $HOME/.local/state/relayx
```

常用覆盖参数：

```bash
BINDIR=/usr/local/bin scripts/install.sh
PREFIX=/opt/relayx scripts/install.sh
CODEX_INSTALL_CMD='brew install --cask codex' scripts/install.sh
scripts/install.sh --dry-run
```

卸载：

```bash
scripts/uninstall.sh
scripts/uninstall.sh --remove-config --remove-state
```

macOS 安装包：

```bash
scripts/package_macos_pkg.sh --version dev --arch arm64 --output-dir dist
sudo installer -pkg dist/relayx-dev-darwin-arm64.pkg -target /
```

`.pkg` 会把 `relayx` 安装到 `/usr/local/bin/relayx`，并把配置模板放到
`/usr/local/share/relayx/relayx.env.example`。卸载时执行
`sudo /usr/local/share/relayx/uninstall.sh`。GitHub Actions 会同时构建
`amd64` 和 `arm64` 的 macOS `.pkg` 产物；推送 `v*` tag 时也会自动附加到
GitHub Release。未签名安装包适合内部测试；如果要面向外部分发并支持更顺滑的双击安装，应使用 Developer ID Installer 证书并做 Apple notarization。

## 快速开始

不启动 Codex，仅在本地运行 RelayX：

```bash
go run ./cmd/relayx check
go run ./cmd/relayx parse "/codex start repo=/tmp/demo fix the failing test"
go run ./cmd/relayx serve
```

在另一个终端里模拟一条飞书消息：

```bash
curl -sS http://127.0.0.1:8787/dev/message \
  -H 'content-type: application/json' \
  -d '{"chat_id":"oc_demo","user_id":"ou_demo","text":"/codex start repo=/tmp/demo fix the failing test"}'
```

启动 RelayX 并启用 Codex app-server：

```bash
RELAYX_CODEX_MODE=app-server go run ./cmd/relayx serve
```

## 配置

RelayX 通过环境变量进行配置。

核心配置：

```bash
RELAYX_LISTEN_ADDR=127.0.0.1:8787
RELAYX_CODEX_MODE=disabled
RELAYX_CODEX_BIN=codex
RELAYX_RUNTIME_DIR=.relayx/run
RELAYX_DB=.relayx/state.json
RELAYX_AUDIT_LOG=.relayx/audit.jsonl
```

Codex 模式：

```text
disabled    Do not start Codex. Useful for local HTTP/dev testing.
app-server  Start `codex app-server --listen stdio://` and drive it via JSON-RPC.
```

`disabled` 模式不会启动 Codex，适合本地 HTTP 或飞书回调测试。

`app-server` 模式会启动 `codex app-server --listen stdio://`，并通过 JSON-RPC 驱动它。

安全控制：

```bash
RELAYX_AUTHORIZED_USERS=ou_xxx,ou_yyy
RELAYX_ALLOWED_REPOS=/Users/me/project-a,/Users/me/project-b
```

飞书配置：

```bash
FEISHU_APP_ID=cli_xxx
FEISHU_APP_SECRET=xxx
FEISHU_VERIFICATION_TOKEN=xxx
FEISHU_BASE_URL=https://open.feishu.cn/open-apis
```

迁移现有本地配置时，旧的 `CODEX_BABYSITTER_*` 环境变量仍会作为 fallback 被兼容读取。

## 飞书接入

创建一个飞书企业自建应用，并启用机器人能力。

配置事件或回调订阅：

```text
im.message.receive_v1
card.action.trigger
```

RelayX 回调地址：

```text
POST /feishu/events
```

RelayX 会处理飞书的 callback URL verification。请将 `FEISHU_VERIFICATION_TOKEN` 配置为与飞书应用后台里的 verification token 一致。

消息链路：

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

如果只测试凭证，RelayX 只需要 `FEISHU_APP_ID` 和 `FEISHU_APP_SECRET`。如果要测试发消息，RelayX 还需要目标 `chat_id`，通常从飞书消息事件中获取。

## 支持的飞书命令

RelayX 当前支持解析这些机器人命令：

```text
/codex start repo=/path/to/repo task description
/codex status
/codex steer additional instruction
/codex stop
/codex diff
/codex logs
/codex help
```

当前行为：

- `start` 会创建任务；在 `app-server` 模式下，还会启动 Codex thread 和 turn。
- `status` 返回当前会话里的最新任务状态。
- `steer` 记录追加指令，并在可行时转发给 Codex。
- `stop` 将最新任务标记为 stopped。
- `diff` 和 `logs` 是预留命令入口，当前返回占位响应。

## Codex 集成

RelayX 通过 stdio JSON-RPC 使用 Codex app-server。

已实现的 Codex 操作：

- `initialize`
- `thread/start`
- `turn/start`
- `turn/steer`
- 命令执行审批响应。
- 文件变更审批响应。
- 旧版 exec 审批响应。
- 用于任务状态更新的基础事件处理。

RelayX 会将 Codex 审批请求映射为飞书卡片。审批决策会映射回 Codex 协议：

```text
approved              -> accept / approved
approved_for_session  -> acceptForSession / approved_for_session
denied                -> decline / denied
abort                 -> cancel / abort
```

## 安全模型

RelayX 默认按保守原则设计。

默认安全属性：

- Codex 保持在本机运行。
- RelayX 默认使用 `RELAYX_CODEX_MODE=disabled`。
- 启用后，Codex app-server 通过 stdio 驱动。
- 用户白名单可以限制谁能控制 RelayX。
- 仓库白名单可以限制可使用的路径。
- 审批卡片只包含摘要。
- 外发消息会经过敏感信息脱敏。
- 状态保存在本地。
- 审计日志是本地 JSONL 文件。

高风险命令由策略层识别。当前风险检测覆盖这些模式：

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

在文本发送到飞书前，敏感信息脱敏会覆盖常见 token 和 key 模式。

## 状态和审计文件

RelayX 会写入本地状态和审计日志：

```text
RELAYX_DB         JSON snapshot of tasks and approvals.
RELAYX_AUDIT_LOG  JSONL audit trail of user actions and approval decisions.
```

`RELAYX_DB` 保存任务和审批的 JSON 快照。

`RELAYX_AUDIT_LOG` 保存用户操作和审批决策的 JSONL 审计记录。

当前持久化实现是文件型实现。代码通过 `core.Snapshot` 和 `persist.FileStateStore` 隔离持久化边界，因此后续可以在不改变 app service 边界的情况下增加 SQLite 存储。

## 开发

运行全部测试：

```bash
go test ./...
go test -race ./...
go vet ./...
```

运行指定包测试：

```bash
go test ./internal/codex
go test ./internal/e2e
```

检查安装脚本：

```bash
bash -n scripts/install.sh
bash -n scripts/uninstall.sh
bash -n scripts/package_macos_pkg.sh
scripts/install.sh --dry-run
```

## 仓库结构

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

目录说明：

- `cmd/relayx/` 包含 CLI 入口。
- `internal/app/` 包含服务编排和任务/审批处理。
- `internal/codex/` 包含 Codex app-server JSON-RPC 适配器。
- `internal/config/` 包含环境变量配置加载。
- `internal/core/` 包含命令、策略、脱敏和任务状态。
- `internal/e2e/` 包含进程内端到端测试。
- `internal/feishu/` 包含飞书 OpenAPI 客户端和回调处理器。
- `internal/httpapi/` 包含开发模式和飞书回调模式共用的 HTTP 路由。
- `internal/persist/` 包含文件型状态和审计日志。
- `scripts/` 包含安装和卸载脚本。
- `docs/` 包含设计说明和里程碑计划。

## 路线图

近期：

- 增加飞书长连接接收适配器。
- 增强 `/codex diff` 和 `/codex logs` 响应。
- 改进 Codex 事件摘要。
- 增加 launchd/systemd 服务模板。
- 增加可选 SQLite 持久化。

后续：

- 支持每个会话多个并发任务路由。
- 更细粒度的审批策略。
- 用于本地检查的 Web dashboard。
- 为不想直接暴露 HTTP callback 的用户提供 cloud relay 模式。

## 故障排查

检查 RelayX 是否识别到飞书配置：

```bash
relayx check
```

测试本地命令解析：

```bash
relayx parse "/codex start repo=/tmp/demo fix bug"
```

如果 Codex 无法启动：

- 确认 `codex` 在 `PATH` 中。
- 运行 `codex --version`。
- 使用 `RELAYX_CODEX_MODE=disabled` 运行 RelayX，以隔离飞书或本地 HTTP 问题。

如果飞书回调没有到达：

- 确认飞书事件订阅包含 `im.message.receive_v1` 和 `card.action.trigger`。
- 确认 callback URL 指向 `/feishu/events`。
- 确认 `FEISHU_VERIFICATION_TOKEN` 与飞书应用设置一致。
- 确认内网穿透或反向代理可以访问 `RELAYX_LISTEN_ADDR`。

如果飞书发送失败：

- 确认 `FEISHU_APP_ID` 和 `FEISHU_APP_SECRET`。
- 确认机器人已安装到目标租户或会话。
- 确认 RelayX 已从入站事件中获得有效 `chat_id`。

## 许可证

见 [LICENSE](LICENSE)。
