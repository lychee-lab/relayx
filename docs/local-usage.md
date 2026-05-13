# RelayX 本地使用与 Codex 启动说明

本文档说明如何在本地最小化启动 RelayX、如何配置运行参数，以及 RelayX 与 Codex CLI 的关系。

## 依赖

本地需要：

```bash
go version
codex --version
```

如果只是测试 RelayX 的本地 HTTP/API 流程，可以先不启用 Codex；真正让 RelayX 执行 Codex 任务时，才需要 `codex` 可用并已完成登录/配置。

## 本地直接运行 RelayX

在仓库根目录执行：

```bash
go run ./cmd/relayx check
go run ./cmd/relayx serve
```

默认配置为：

```text
RELAYX_LISTEN_ADDR=127.0.0.1:8787
RELAYX_CODEX_MODE=disabled
RELAYX_CODEX_BIN=codex
RELAYX_DB=.relayx/state.json
RELAYX_AUDIT_LOG=.relayx/audit.jsonl
```

`RELAYX_CODEX_MODE=disabled` 时，RelayX 只启动本地服务，不会启动 Codex，适合先调试消息入口和命令解析。

## 模拟本地消息

RelayX 启动后，在另一个终端执行：

```bash
curl -sS http://127.0.0.1:8787/dev/message \
  -H 'content-type: application/json' \
  -d '{"chat_id":"oc_demo","user_id":"ou_demo","text":"/codex start repo=/tmp/demo fix the failing test"}'
```

支持的命令：

```text
/codex start repo=/path/to/repo task description
/codex start repo=/path/to/repo model=gpt-5.2 effort=high task description
/codex status
/codex steer additional instruction
/model list
/model <model-id> [effort=low|medium|high|xhigh]
/fast [model=<model-id>] [effort=low]
/review [base=<branch>|commit=<sha>|detached]
/resume [repo=/path] [limit=5]
/codex stop
/codex diff
/codex logs
/codex help
```

## 启用 Codex

RelayX 不会连接一个已经打开的交互式 `codex` 终端会话。本地有一个活跃的 Codex CLI 会话不够，也不需要。

当前实现中，当设置：

```bash
RELAYX_CODEX_MODE=app-server
```

RelayX 会在启动时自己执行：

```bash
codex app-server --listen stdio://
```

然后通过 stdio JSON-RPC 与这个 Codex 子进程通信。

启动方式：

```bash
RELAYX_CODEX_MODE=app-server go run ./cmd/relayx serve
```

如果 `codex` 不在默认 `PATH`，指定绝对路径：

```bash
RELAYX_CODEX_MODE=app-server \
RELAYX_CODEX_BIN=/path/to/codex \
go run ./cmd/relayx serve
```

然后发送任务：

```bash
curl -sS http://127.0.0.1:8787/dev/message \
  -H 'content-type: application/json' \
  -d '{"chat_id":"oc_demo","user_id":"ou_demo","text":"/codex start repo=/Users/you/project fix the failing test"}'
```

## 模型、快速模式、Review 和 Resume

RelayX 支持把飞书中的 `/model`、`/fast`、`/review`、`/resume` 转成 Codex app-server 的控制请求，而不是把这些文本当作普通 prompt 发给模型。

查看 Codex 当前可选模型：

```text
/model list
```

设置当前 chat 后续 turn 使用的模型：

```text
/model gpt-5.2
/model gpt-5.2 effort=high
/model model=gpt-5.4-mini effort=low
```

查看当前 chat 的模型设置：

```text
/model current
```

`effort` 可用值：

```text
none
minimal
low
medium
high
xhigh
```

`/model` 的模型 ID 不在 RelayX 中硬编码；推荐先用 `/model list` 从 Codex 查询，再把返回的模型 ID 发给 `/model <id>`。RelayX 会保存该 chat 的模型设置，并在后续 `turn/start` 时传给 Codex。正在运行中的当前 turn 不会被无损原地切换；新的设置会作用到下一轮，或者在上一轮完成后通过 `/codex steer ...` 开启的新 turn。

快速模式：

```text
/fast
/fast model=gpt-5.4-mini
/fast effort=minimal
```

默认 `/fast` 会把后续 turn 的 `effort` 设为 `low`。如果同时传 `model=...`，也会更新后续 turn 使用的模型。

Review：

```text
/review
/review base=main
/review commit=abc123
/review detached
```

`/review` 需要已有任务和 Codex thread，因此通常先执行 `/codex start repo=... ...`，再在同一个飞书会话里触发 `/review`。

Resume：

```text
/resume
/resume repo=/Users/you/project limit=5
/resume <thread_id>
```

`/resume` 会调用 Codex app-server 的 `thread/list` 获取可恢复 session。飞书模式下 RelayX 会发送一张可点击卡片；点击某个 session 后，RelayX 会调用 `thread/resume`，并把恢复后的 thread 注册为当前飞书会话的最新任务。随后可以继续发送：

```text
/codex steer 继续处理这个问题
```

本地 `/dev/message` 调试时没有飞书卡片，RelayX 会直接返回文本列表；可以再用 `/resume <thread_id>` 选择恢复。

## 可选安全限制

可以限制允许控制 RelayX 的飞书用户，以及允许 Codex 操作的仓库路径：

```bash
RELAYX_AUTHORIZED_USERS=ou_xxx
RELAYX_ALLOWED_REPOS=/Users/you/project-a,/Users/you/project-b
RELAYX_CODEX_MODE=app-server go run ./cmd/relayx serve
```

如果配置了 `RELAYX_AUTHORIZED_USERS`，只有列出的用户 ID 可以发起或审批任务。

如果配置了 `RELAYX_ALLOWED_REPOS`，`/codex start repo=...` 中的路径必须在允许列表内。

## 安装成命令使用

也可以安装为本地命令：

```bash
scripts/install.sh
```

默认位置：

```text
Binary:  $HOME/.local/bin/relayx
Config:  $HOME/.config/relayx/relayx.env
State:   $HOME/.local/state/relayx
```

注意：程序只读取环境变量，不会自动加载 `relayx.env`。启动前需要手动加载：

```bash
set -a
. ~/.config/relayx/relayx.env
set +a
relayx check
relayx serve
```

## 飞书配置

只有接入飞书时才需要配置：

```bash
FEISHU_APP_ID=cli_xxx
FEISHU_APP_SECRET=xxx
FEISHU_VERIFICATION_TOKEN=xxx
FEISHU_BASE_URL=https://open.feishu.cn/open-apis
```

飞书回调地址配置到：

```text
POST /feishu/events
```

使用飞书 HTTP callback 模式时，需要通过公网 relay、内网穿透或反向代理，让飞书能够访问到本地 RelayX 服务。
