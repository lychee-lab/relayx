# M1 落地方案：控制核心与本地开发入口

## 目标

先交付一个可运行、可测试、不依赖飞书凭证和 Codex 在线行为的最小核心：

- 解析 `/codex ...` 指令。
- 管理任务状态。
- 暴露本地开发 HTTP 入口模拟飞书消息。
- 形成后续 Feishu adapter 和 Codex adapter 可接入的内部接口。

这一阶段不直接调用飞书 SDK，也不启动真实 Codex app-server。这样可以先把核心协议、状态流和测试边界稳定下来。

## 范围

包含：

- Go module 初始化。
- `cmd/relayx` CLI。
- `check`：打印配置检查结果。
- `parse`：解析一条 `/codex` 指令。
- `serve`：启动本地 HTTP dev adapter。
- `POST /dev/message`：模拟飞书消息输入。
- `GET /healthz`：健康检查。
- command parser 单测。
- task manager 单测。

不包含：

- Feishu SDK 长连接。
- Feishu OpenAPI 发消息。
- Codex app-server JSON-RPC。
- 文件型或 SQLite 持久化。
- launchd/systemd 打包。

## 内部接口

输入消息：

```go
type InboundMessage struct {
    ChatID string
    UserID string
    Text   string
}
```

解析后的命令：

```go
type Command struct {
    Action Action
    Repo   string
    Text   string
}
```

任务：

```go
type Task struct {
    ID     string
    ChatID string
    UserID string
    Repo   string
    Prompt string
    Status TaskStatus
}
```

后续 M2/M4 会分别把 Codex 和 Feishu 接到这些接口上。

## 验收标准

- `go test ./...` 通过。
- `go run ./cmd/relayx check` 能输出配置摘要。
- `go run ./cmd/relayx parse "/codex start repo=/tmp/demo fix bug"` 能输出 JSON。
- `go run ./cmd/relayx serve` 后，`POST /dev/message` 能创建任务并返回任务 ID。
- 无外部依赖，首次落地不需要网络下载。

## 风险与处理

- Go 1.20 较旧：第一阶段只使用标准库，避免新语言特性。
- Feishu SDK 之后引入可能影响接口：M1 用 adapter interface 隔离外部 SDK。
- Codex app-server 协议仍是 experimental：M2 单独封装协议层，核心状态机不直接依赖 generated schema。
