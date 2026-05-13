# RelayX 技术设计

## 1. 背景

目标是做一个 Codex CLI 的外围控制应用，让用户可以在不在电脑旁时，通过手机飞书查看 Codex 关键进展、确认权限、追加指令、暂停或终止任务。

该系统必须保持本地优先：Codex 仍运行在用户电脑或开发机上，仓库、凭证和 shell 权限不直接暴露给云端或飞书。飞书只承担通知、审批和轻量指令入口。

## 2. 目标与非目标

目标：

- 将 Codex 的任务状态、关键输出、审批请求同步到飞书。
- 支持用户从飞书发起新任务、追加指令、查询状态、拒绝或批准权限。
- 支持 Codex app-server JSON-RPC 协议作为主接入路径。
- 保留 `codex exec --json` 作为非交互或兼容降级路径。
- 用本地策略控制仓库范围、用户范围、命令风险、审批有效期和日志脱敏。
- 提供可审计的本地状态存储。

非目标：

- 不做完整移动端 IDE。
- 不把本机 Codex app-server 直接暴露到公网。
- 不在第一阶段支持多租户 SaaS。
- 不绕过 Codex 自身 sandbox 和 approval 机制。
- 不支持无授权用户通过飞书控制任意仓库。

## 3. 语言选择

选择 Go。

原则：

- 长驻本地 daemon 要稳定，部署要简单。
- 需要同时处理飞书长连接、Codex JSON-RPC、进程生命周期、任务状态机和超时。
- 后续需要易于打包成单二进制并放进 launchd/systemd。
- 飞书官方 Go SDK 可覆盖长连接事件、卡片回调和 OpenAPI 调用。

备选判断：

- Python：开发快，飞书 SDK 可用，但长期 daemon、并发取消、单文件部署和类型约束弱一些。
- Rust：运行时安全强，但飞书生态和开发速度不如 Go，现阶段投入产出比低。
- Go：并发模型、二进制部署、SDK 生态、可维护性最均衡。

## 4. 总体架构

```text
Feishu mobile/client
  -> Feishu bot message / interactive card callback
  -> Feishu adapter
  -> Command router / approval router
  -> Task manager / policy engine / audit log
  -> Codex adapter
  -> Codex app-server JSON-RPC
  -> local repository and shell
```

本地进程：

```text
relayx
  - Feishu long-connection client
  - Feishu OpenAPI client
  - Codex app-server supervisor
  - Codex JSON-RPC client
  - File-backed state snapshot
  - Policy engine
  - Local HTTP dev adapter

codex app-server
  - launched or attached by relayx
  - listens on unix socket or 127.0.0.1 only
```

## 5. Codex 接入设计

主路径：`codex app-server`。

本机 `codex` 暴露了 `app-server`、`remote-control`、`exec --json` 等能力；`app-server generate-json-schema` 可生成协议 schema，其中包含：

- `thread/start`
- `thread/resume`
- `turn/start`
- `turn/steer`
- `thread/injectItems`
- `turn/completed`
- command approval response
- file change approval response
- permission approval response

生产接入方式：

- 启动 `codex app-server --listen unix://$RUNTIME_DIR/codex.sock`。
- 如果 Unix socket 在目标平台不稳定，则使用 `ws://127.0.0.1:$PORT`。
- relayx 通过 JSON-RPC 2.0 发送请求并监听通知。
- 每个飞书会话映射一个 Codex thread。
- 新任务调用 `thread/start` 后调用 `turn/start`。
- 任务执行中追加指令优先使用 `turn/steer`。
- 审批请求由 Codex adapter 转成内部 `ApprovalRequest`。
- 飞书按钮回调转成 Codex approval response。

降级路径：`codex exec --json`。

- 用于单轮、非交互、无审批或低风险任务。
- JSONL 输出映射成事件流。
- 不作为长期交互主路径，因为 turn steer、精细审批和 thread 生命周期能力较弱。

不推荐路径：PTY 包装 Codex TUI。

- 只作为最后 fallback。
- 解析终端 UI 容易受输出格式、alt-screen、颜色和快捷键影响。

## 6. 飞书接入设计

使用企业自建应用，启用机器人能力。

事件入口：

- `im.message.receive_v1`：接收用户私聊或群聊中的 `/codex ...` 指令。
- `card.action.trigger`：接收交互卡片按钮点击，用于批准、拒绝、终止、查看详情等操作。

当前实现：

- 已实现 HTTP callback receiver：`POST /feishu/events`。
- 支持 URL verification、`im.message.receive_v1` 和 `card.action.trigger`。
- 本地无公网 IP 时，可用内网穿透或云 relay 将该 callback 转发到本机。

推荐生产订阅方式：

- 优先使用飞书 SDK 长连接。
- 本地服务只需能访问公网，不需要公网 IP、域名或内网穿透。
- 长连接模式下，如果同一应用启动多个 client，事件是集群随机投递，不是广播，所以生产环境同一个 Feishu App 只运行一个 active receiver，或用分布式锁保证单活。

发送消息：

- 获取 `tenant_access_token`。
- 调用 `/im/v1/messages` 发送文本、富文本或交互卡片。
- 审批请求用交互卡片。
- 长日志只发摘要，完整内容保存在本地。

飞书 3 秒回调约束：

- 卡片回调必须快速响应。
- 回调 handler 只做鉴权、幂等检查、落库、入队，然后立即返回 toast 或卡片更新。
- 实际调用 Codex approval response 异步执行。

## 7. 用户交互协议

命令：

```text
/codex start repo=/path/to/repo 需求描述
/codex status
/codex steer 追加指令
/codex stop
/codex diff
/codex logs
/codex help
```

审批卡片按钮：

- `批准一次`
- `本轮批准`
- `拒绝`
- `终止任务`
- `查看详情`

高风险命令限制：

- `rm -rf`
- `git reset --hard`
- `git clean -fd`
- `sudo`
- `chmod -R`
- `curl | sh`
- `danger-full-access`
- 涉及 `.env`、私钥、token 文件的读取或输出

这些操作默认不允许“本轮批准”，只能单次确认，或完全禁止。

## 8. 状态模型

任务状态：

```text
created -> running -> waiting_approval -> running -> completed
                  \-> denied -> running
                  \-> aborted
running -> paused
running -> failed
running -> stopped
```

核心实体：

- `Session`：飞书 chat/user 与 Codex thread 的映射。
- `Turn`：一次用户指令或追加指令。
- `ApprovalRequest`：一次权限确认。
- `AuditLog`：所有外部输入、审批决策和 Codex 高风险动作。

当前持久化设计：

- `~/.relayx/state.json` 是默认 JSON state snapshot，保存 task、thread、turn、approval 的可恢复状态。
- `~/.relayx/logs/audit.jsonl` 是默认 JSONL audit log，逐行记录用户输入、审批决策和关键控制动作。
- `core.Snapshot` 与 `persist.FileStateStore` 是持久化边界，后续如果需要强事务和查询能力，可在该边界下替换为 SQLite。

## 9. 策略与安全

默认安全策略：

- Codex app-server 只监听 Unix socket 或 loopback。
- 不将 Codex app-server 直接暴露公网。
- Feishu 用户白名单控制可操作人员。
- 仓库路径白名单控制可操作目录。
- Codex 默认 `workspace-write`。
- 高风险动作必须单次审批。
- 审批默认 10 分钟过期。
- 所有审批决策必须写审计日志。
- 输出日志必须脱敏。
- `.env`、SSH key、token、cookie、证书内容默认不发送到飞书。

权限策略分层：

- Feishu actor policy：谁可以控制。
- Repository policy：能控制哪些路径。
- Command risk policy：哪些命令需要审批、禁止或允许。
- Output policy：哪些内容允许发到飞书。
- Session policy：任务超时、空闲回收、审批过期。

## 10. 错误处理

Codex app-server 异常：

- supervisor 自动重启一次。
- 如果 thread 可恢复，则 `thread/resume`。
- 如果不可恢复，向飞书发送失败摘要并保留本地日志路径。

飞书发送失败：

- OpenAPI 失败按错误类型重试。
- 限流时退避。
- 关键审批请求必须落库，发送失败后可通过 `/codex status` 重新拉取。

长连接断开：

- SDK 自动重连。
- 超过阈值向本地日志报警。
- 断开期间 Codex 可以继续运行，但审批会进入 pending；需要超时策略避免无限等待。

回调重复：

- 所有 card action 使用 action id + approval id 幂等。
- 已处理的审批再次点击只返回当前状态。

## 11. 可观测性

日志：

- 本地结构化 JSON log。
- 每个 session、turn、approval 都有 correlation id。
- 飞书只发送摘要，不发送全量 debug log。

指标：

- active sessions
- running turns
- pending approvals
- approval latency
- Feishu API error count
- Codex adapter reconnect count
- redacted output count

调试入口：

- `GET /healthz`
- `GET /debug/sessions`
- `POST /dev/message`

debug endpoint 默认只绑定 `127.0.0.1`，生产需要显式开启。

## 12. 部署方式

本地开发：

```bash
go run ./cmd/relayx serve
```

macOS 生产：

- 打包单二进制。
- 用 launchd 常驻。
- `~/.relayx/run` 放 socket 或运行时文件。
- `~/.relayx/state.json` 放状态快照。

Linux 生产：

- systemd user service。
- RuntimeDirectory 存放 socket。
- StateDirectory 存放状态快照和日志。

配置来源：

- 环境变量。
- 后续可增加 TOML 配置文件。

## 13. 里程碑

M1：控制核心和本地开发入口。

- Go 项目骨架。
- `/codex` 指令解析。
- 内存 task manager。
- 本地 HTTP dev adapter。
- 基础测试。
- 状态：已完成。

M2：Codex app-server adapter。

- 启动/连接 app-server。
- JSON-RPC request/notification。
- thread/turn 生命周期。
- Codex event -> internal event。
- 状态：已完成 stdio JSON-RPC adapter。

M3：飞书发送消息。

- tenant token 管理。
- 文本消息和交互卡片。
- 本地事件转飞书通知。
- 状态：已完成 OpenAPI client。

M4：飞书长连接接收。

- `im.message.receive_v1`。
- `card.action.trigger`。
- 回调幂等和 3 秒 ack。
- 状态：已完成 HTTP callback receiver；长连接 receiver 可作为同一 service 的替代输入适配器接入。

M5：审批闭环。

- Codex approval -> Feishu card。
- Feishu button -> Codex decision。
- 审批过期、拒绝、终止。
- 状态：已完成核心闭环和端到端测试。

M6：持久化和安全策略。

- 文件型状态快照，后续可替换为 SQLite。
- 用户白名单。
- repo 白名单。
- secret redaction。
- audit log。
- 状态：已完成用户白名单、repo 白名单、secret redaction、文件型状态快照和 JSONL audit log。当前实现没有引入 SQLite driver，避免第一版依赖外部 C/CGO 或大体积纯 Go driver；`core.Snapshot` 和 `persist.FileStateStore` 保留了后续替换 SQLite 的边界。

M7：打包部署。

- launchd/systemd。
- 配置模板。
- 操作文档。

## 14. 参考

- Codex CLI: https://github.com/openai/codex
- 飞书长连接接收事件: https://feishu.apifox.cn/doc-7518429
- 飞书接收回调: https://feishu.apifox.cn/doc-7518486
- 飞书发送消息: https://apifox.com/apidoc/docs-site/532425/api-58348294
- 飞书 tenant access token: https://feishu.apifox.cn/api-58156651
