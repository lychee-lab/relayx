# RelayX

RelayX is a local-first control plane for driving Codex CLI from Feishu.

RelayX 是一个本地优先的控制平面，用来通过飞书远程驱动 Codex CLI。

It runs next to Codex on your development machine, forwards important Codex
events to Feishu, and lets you approve permissions or send follow-up instructions
from a phone.

它运行在你的开发机上、位于 Codex 旁边，将重要的 Codex 事件转发到飞书，并允许你在手机上批准权限或发送追加指令。

The goal is to keep the actual code, shell, credentials, and repository access
local while using Feishu as a remote command and approval surface.

目标是让代码、Shell、凭证和仓库访问都保留在本机，同时把飞书作为远程指令和审批入口。

## Why

为什么

Codex CLI is most useful when it can inspect a repo, run tests, edit files, and
ask for permission before risky operations.

当 Codex CLI 能够检查仓库、运行测试、编辑文件，并在高风险操作前请求权限时，它的价值最大。

That workflow usually assumes you are at the terminal.

但这套工作流通常默认你就在电脑和终端旁边。

RelayX adds a small local service around Codex so you can:

RelayX 在 Codex 外围增加一个小型本地服务，让你可以：

- Start a Codex task from Feishu.
- 从飞书启动 Codex 任务。
- See task status and important Codex events in Feishu.
- 在飞书里查看任务状态和重要 Codex 事件。
- Approve or reject command, file, and permission requests from Feishu cards.
- 通过飞书卡片批准或拒绝命令、文件和权限请求。
- Send additional instructions while a task is running.
- 在任务运行过程中发送追加指令。
- Keep Codex, repositories, and secrets on the local machine.
- 让 Codex、仓库和敏感信息继续保留在本机。

## Current Status

当前状态

RelayX currently includes:

RelayX 当前已经包含：

- Go CLI binary: `relayx`.
- Go CLI 二进制：`relayx`。
- Local HTTP API for development and Feishu callbacks.
- 用于开发调试和飞书回调的本地 HTTP API。
- Codex app-server stdio JSON-RPC adapter.
- Codex app-server 的 stdio JSON-RPC 适配器。
- Feishu OpenAPI client for text messages and interactive approval cards.
- 用于文本消息和交互式审批卡片的飞书 OpenAPI 客户端。
- Feishu HTTP callback handler for URL verification, bot messages, and card actions.
- 支持 URL verification、机器人消息和卡片操作的飞书 HTTP 回调处理器。
- `/codex` command parser.
- `/codex` 指令解析器。
- In-memory task and approval state manager.
- 内存中的任务和审批状态管理器。
- File-backed state snapshot.
- 文件型状态快照。
- JSONL audit log.
- JSONL 审计日志。
- User and repo allowlist policy.
- 用户和仓库白名单策略。
- Secret redaction for outbound messages.
- 外发消息的敏感信息脱敏。
- Unit tests, race tests, and an in-process end-to-end test.
- 单元测试、race 测试和进程内端到端测试。

The Feishu receive path is currently implemented through HTTP callbacks.

飞书接收链路当前通过 HTTP callback 实现。

A Feishu long-connection receiver can be added later as another adapter over the
same app service.

后续可以在同一套 app service 之上增加飞书长连接接收器，作为另一个输入适配器。

## Architecture

架构

The high-level flow is:

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

Runtime processes:

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

RelayX does not expose Codex app-server directly to the public internet.

RelayX 不会把 Codex app-server 直接暴露到公网。

Feishu only talks to RelayX, and RelayX talks to Codex locally.

飞书只和 RelayX 通信，RelayX 再在本机和 Codex 通信。

## Requirements

依赖要求

- Go 1.20 or newer.
- Go 1.20 或更高版本。
- Codex CLI.
- Codex CLI。
- Feishu app credentials if you want Feishu integration.
- 如果需要飞书集成，需要飞书应用凭证。
- A reachable HTTP callback URL for Feishu callback mode, such as a public relay,
  tunnel, or reverse proxy pointing to RelayX.
- 使用飞书 HTTP callback 模式时，需要一个可被飞书访问的回调 URL，例如公网 relay、内网穿透或指向 RelayX 的反向代理。

On macOS, the installer can install Codex CLI through Homebrew if `codex` is not
already available.

在 macOS 上，如果本机还没有 `codex`，安装脚本可以通过 Homebrew 安装 Codex CLI。

## Install

安装

From the repo root:

在仓库根目录执行：

```bash
scripts/install.sh
```

The installer:

安装脚本会：

- Checks whether `codex` is available.
- 检查 `codex` 是否可用。
- Installs Codex CLI first on macOS when Homebrew is present and Codex is missing.
- 在 macOS 且存在 Homebrew、但缺少 Codex 时，先安装 Codex CLI。
- Builds `relayx`.
- 构建 `relayx`。
- Installs the binary to the default user program directory.
- 将二进制安装到默认用户程序目录。
- Writes a config template.
- 写入配置模板。
- Creates the default state directory.
- 创建默认状态目录。

Default locations:

默认位置：

```text
Binary:  $HOME/.local/bin/relayx
Config:  $HOME/.config/relayx/relayx.env
State:   $HOME/.local/state/relayx
```

Useful overrides:

常用覆盖参数：

```bash
BINDIR=/usr/local/bin scripts/install.sh
PREFIX=/opt/relayx scripts/install.sh
CODEX_INSTALL_CMD='brew install --cask codex' scripts/install.sh
scripts/install.sh --dry-run
```

Uninstall:

卸载：

```bash
scripts/uninstall.sh
scripts/uninstall.sh --remove-config --remove-state
```

## Quick Start

快速开始

Run locally without starting Codex:

不启动 Codex，仅在本地运行 RelayX：

```bash
go run ./cmd/relayx check
go run ./cmd/relayx parse "/codex start repo=/tmp/demo fix the failing test"
go run ./cmd/relayx serve
```

In another terminal, simulate a Feishu message:

在另一个终端里模拟一条飞书消息：

```bash
curl -sS http://127.0.0.1:8787/dev/message \
  -H 'content-type: application/json' \
  -d '{"chat_id":"oc_demo","user_id":"ou_demo","text":"/codex start repo=/tmp/demo fix the failing test"}'
```

Start RelayX with Codex app-server enabled:

启动 RelayX 并启用 Codex app-server：

```bash
RELAYX_CODEX_MODE=app-server go run ./cmd/relayx serve
```

## Configuration

配置

RelayX is configured through environment variables.

RelayX 通过环境变量进行配置。

Core settings:

核心配置：

```bash
RELAYX_LISTEN_ADDR=127.0.0.1:8787
RELAYX_CODEX_MODE=disabled
RELAYX_CODEX_BIN=codex
RELAYX_RUNTIME_DIR=.relayx/run
RELAYX_DB=.relayx/state.json
RELAYX_AUDIT_LOG=.relayx/audit.jsonl
```

Codex mode:

Codex 模式：

```text
disabled    Do not start Codex. Useful for local HTTP/dev testing.
app-server  Start `codex app-server --listen stdio://` and drive it via JSON-RPC.
```

The `disabled` mode does not start Codex and is useful for local HTTP or Feishu
callback testing.

`disabled` 模式不会启动 Codex，适合本地 HTTP 或飞书回调测试。

The `app-server` mode starts `codex app-server --listen stdio://` and drives it
through JSON-RPC.

`app-server` 模式会启动 `codex app-server --listen stdio://`，并通过 JSON-RPC 驱动它。

Safety controls:

安全控制：

```bash
RELAYX_AUTHORIZED_USERS=ou_xxx,ou_yyy
RELAYX_ALLOWED_REPOS=/Users/me/project-a,/Users/me/project-b
```

Feishu settings:

飞书配置：

```bash
FEISHU_APP_ID=cli_xxx
FEISHU_APP_SECRET=xxx
FEISHU_VERIFICATION_TOKEN=xxx
FEISHU_BASE_URL=https://open.feishu.cn/open-apis
```

Legacy `CODEX_BABYSITTER_*` environment variables are still accepted as fallback
while migrating existing local configs to `RELAYX_*`.

迁移现有本地配置时，旧的 `CODEX_BABYSITTER_*` 环境变量仍会作为 fallback 被兼容读取。

## Feishu Setup

飞书接入

Create a Feishu internal app and enable bot capabilities.

创建一个飞书企业自建应用，并启用机器人能力。

Configure event/callback subscriptions:

配置事件或回调订阅：

```text
im.message.receive_v1
card.action.trigger
```

RelayX callback endpoint:

RelayX 回调地址：

```text
POST /feishu/events
```

Callback URL verification is handled by RelayX.

RelayX 会处理飞书的 callback URL verification。

Configure `FEISHU_VERIFICATION_TOKEN` to match the token configured in Feishu.

请将 `FEISHU_VERIFICATION_TOKEN` 配置为与飞书应用后台里的 verification token 一致。

Message flow:

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

To test only credentials, RelayX needs `FEISHU_APP_ID` and `FEISHU_APP_SECRET`.

如果只测试凭证，RelayX 只需要 `FEISHU_APP_ID` 和 `FEISHU_APP_SECRET`。

To test sending a message, RelayX also needs a target `chat_id`, which is normally
obtained from an incoming Feishu message event.

如果要测试发消息，RelayX 还需要目标 `chat_id`，通常从飞书消息事件中获取。

## Supported Feishu Commands

支持的飞书命令

RelayX currently parses these bot commands:

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

Current behavior:

当前行为：

- `start` creates a task and, in `app-server` mode, starts a Codex thread and turn.
- `start` 会创建任务；在 `app-server` 模式下，还会启动 Codex thread 和 turn。
- `status` returns the latest task status in the chat.
- `status` 返回当前会话里的最新任务状态。
- `steer` records an additional instruction and, when possible, forwards it to Codex.
- `steer` 记录追加指令，并在可行时转发给 Codex。
- `stop` marks the latest task as stopped.
- `stop` 将最新任务标记为 stopped。
- `diff` and `logs` are reserved command surfaces and currently return placeholder responses.
- `diff` 和 `logs` 是预留命令入口，当前返回占位响应。

## Codex Integration

Codex 集成

RelayX uses Codex app-server over stdio JSON-RPC.

RelayX 通过 stdio JSON-RPC 使用 Codex app-server。

Implemented Codex operations:

已实现的 Codex 操作：

- `initialize`
- `initialize`
- `thread/start`
- `thread/start`
- `turn/start`
- `turn/start`
- `turn/steer`
- `turn/steer`
- command execution approval response
- 命令执行审批响应。
- file change approval response
- 文件变更审批响应。
- legacy exec approval response
- 旧版 exec 审批响应。
- basic event handling for task state updates
- 用于任务状态更新的基础事件处理。

RelayX maps Codex approval requests into Feishu cards.

RelayX 会将 Codex 审批请求映射为飞书卡片。

Approval decisions are mapped back into the Codex protocol:

审批决策会映射回 Codex 协议：

```text
approved              -> accept / approved
approved_for_session  -> acceptForSession / approved_for_session
denied                -> decline / denied
abort                 -> cancel / abort
```

## Security Model

安全模型

RelayX is designed to be conservative by default.

RelayX 默认按保守原则设计。

Default safety properties:

默认安全属性：

- Codex stays local.
- Codex 保持在本机运行。
- RelayX defaults to `RELAYX_CODEX_MODE=disabled`.
- RelayX 默认使用 `RELAYX_CODEX_MODE=disabled`。
- Codex app-server is driven through stdio when enabled.
- 启用后，Codex app-server 通过 stdio 驱动。
- User allowlist can restrict who can control RelayX.
- 用户白名单可以限制谁能控制 RelayX。
- Repo allowlist can restrict which paths can be used.
- 仓库白名单可以限制可使用的路径。
- Approval cards include only summaries.
- 审批卡片只包含摘要。
- Outbound messages are passed through secret redaction.
- 外发消息会经过敏感信息脱敏。
- State is local.
- 状态保存在本地。
- Audit log is local JSONL.
- 审计日志是本地 JSONL 文件。

High-risk commands are identified by the policy layer.

高风险命令由策略层识别。

Current risk detection covers patterns such as:

当前风险检测覆盖这些模式：

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

在文本发送到飞书前，敏感信息脱敏会覆盖常见 token 和 key 模式。

## State And Audit Files

状态和审计文件

RelayX writes local state and audit logs:

RelayX 会写入本地状态和审计日志：

```text
RELAYX_DB         JSON snapshot of tasks and approvals.
RELAYX_AUDIT_LOG  JSONL audit trail of user actions and approval decisions.
```

`RELAYX_DB` stores a JSON snapshot of tasks and approvals.

`RELAYX_DB` 保存任务和审批的 JSON 快照。

`RELAYX_AUDIT_LOG` stores a JSONL audit trail of user actions and approval
decisions.

`RELAYX_AUDIT_LOG` 保存用户操作和审批决策的 JSONL 审计记录。

The current persistence implementation is file-backed.

当前持久化实现是文件型实现。

The code keeps persistence behind `core.Snapshot` and `persist.FileStateStore`, so
a future SQLite-backed store can be added without changing the app service
boundary.

代码通过 `core.Snapshot` 和 `persist.FileStateStore` 隔离持久化边界，因此后续可以在不改变 app service 边界的情况下增加 SQLite 存储。

## Development

开发

Run all tests:

运行全部测试：

```bash
go test ./...
go test -race ./...
go vet ./...
```

Run a specific package:

运行指定包测试：

```bash
go test ./internal/codex
go test ./internal/e2e
```

Check install scripts:

检查安装脚本：

```bash
bash -n scripts/install.sh
bash -n scripts/uninstall.sh
scripts/install.sh --dry-run
```

## Repository Layout

仓库结构

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

Directory descriptions:

目录说明：

- `cmd/relayx/` contains the CLI entrypoint.
- `cmd/relayx/` 包含 CLI 入口。
- `internal/app/` contains service orchestration and task/approval handling.
- `internal/app/` 包含服务编排和任务/审批处理。
- `internal/codex/` contains the Codex app-server JSON-RPC adapter.
- `internal/codex/` 包含 Codex app-server JSON-RPC 适配器。
- `internal/config/` contains environment config loading.
- `internal/config/` 包含环境变量配置加载。
- `internal/core/` contains commands, policy, redaction, and task state.
- `internal/core/` 包含命令、策略、脱敏和任务状态。
- `internal/e2e/` contains in-process end-to-end tests.
- `internal/e2e/` 包含进程内端到端测试。
- `internal/feishu/` contains the Feishu OpenAPI client and callback handler.
- `internal/feishu/` 包含飞书 OpenAPI 客户端和回调处理器。
- `internal/httpapi/` contains HTTP routes shared by dev and Feishu callback mode.
- `internal/httpapi/` 包含开发模式和飞书回调模式共用的 HTTP 路由。
- `internal/persist/` contains file-backed state and audit log.
- `internal/persist/` 包含文件型状态和审计日志。
- `scripts/` contains install and uninstall scripts.
- `scripts/` 包含安装和卸载脚本。
- `docs/` contains design notes and milestone plans.
- `docs/` 包含设计说明和里程碑计划。

## Roadmap

路线图

Near-term:

近期：

- Add a Feishu long-connection receiver adapter.
- 增加飞书长连接接收适配器。
- Add richer `/codex diff` and `/codex logs` responses.
- 增强 `/codex diff` 和 `/codex logs` 响应。
- Improve Codex event summarization.
- 改进 Codex 事件摘要。
- Add launchd/systemd service templates.
- 增加 launchd/systemd 服务模板。
- Add optional SQLite persistence.
- 增加可选 SQLite 持久化。

Later:

后续：

- Multiple concurrent task routing per chat.
- 支持每个会话多个并发任务路由。
- More granular approval policies.
- 更细粒度的审批策略。
- Web dashboard for local inspection.
- 用于本地检查的 Web dashboard。
- Cloud relay mode for users who do not want to expose an HTTP callback directly.
- 为不想直接暴露 HTTP callback 的用户提供 cloud relay 模式。

## Troubleshooting

故障排查

Check whether RelayX sees Feishu configuration:

检查 RelayX 是否识别到飞书配置：

```bash
relayx check
```

Test local command parsing:

测试本地命令解析：

```bash
relayx parse "/codex start repo=/tmp/demo fix bug"
```

If Codex cannot start:

如果 Codex 无法启动：

- Confirm `codex` is on `PATH`.
- 确认 `codex` 在 `PATH` 中。
- Run `codex --version`.
- 运行 `codex --version`。
- Run RelayX with `RELAYX_CODEX_MODE=disabled` to isolate Feishu/local HTTP issues.
- 使用 `RELAYX_CODEX_MODE=disabled` 运行 RelayX，以隔离飞书或本地 HTTP 问题。

If Feishu callbacks do not arrive:

如果飞书回调没有到达：

- Confirm Feishu event subscriptions include `im.message.receive_v1` and `card.action.trigger`.
- 确认飞书事件订阅包含 `im.message.receive_v1` 和 `card.action.trigger`。
- Confirm callback URL points to `/feishu/events`.
- 确认 callback URL 指向 `/feishu/events`。
- Confirm `FEISHU_VERIFICATION_TOKEN` matches Feishu app settings.
- 确认 `FEISHU_VERIFICATION_TOKEN` 与飞书应用设置一致。
- Confirm your tunnel or reverse proxy can reach `RELAYX_LISTEN_ADDR`.
- 确认内网穿透或反向代理可以访问 `RELAYX_LISTEN_ADDR`。

If Feishu sending fails:

如果飞书发送失败：

- Confirm `FEISHU_APP_ID` and `FEISHU_APP_SECRET`.
- 确认 `FEISHU_APP_ID` 和 `FEISHU_APP_SECRET`。
- Confirm the bot is installed in the target tenant/chat.
- 确认机器人已安装到目标租户或会话。
- Confirm RelayX has a valid `chat_id` from an incoming event.
- 确认 RelayX 已从入站事件中获得有效 `chat_id`。

## License

许可证

See [LICENSE](LICENSE).

见 [LICENSE](LICENSE)。
