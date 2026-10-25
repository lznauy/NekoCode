# acp — ACP v1 Agent 适配层

`acp/` 是一个纯 Go 包，把 NekoCode 的运行时（`runtime`）暴露成 **Agent Client Protocol（ACP）v1** Agent。
协议标准总览与实现状态见 [`docs/developers/ACP.md`](../docs/developers/ACP.md)；本文聚焦包内实现细节。
它负责：

- 在一条 **stdio** 上维护一个按行（newline-delimited）的 JSON-RPC 2.0 连接；
- 把 ACP 的会话与提示（prompt）请求翻译成后端 `runtime` 的调用；
- 把运行时的流式事件翻译成 `session/update` 通知回推给客户端；
- 通过 `session/request_permission` 请求客户端做权限审批。

入口在 [`cmd/tui`](../cmd/tui/main.go)：`nekocode-tui --acp` 会走 `acp.RunStdio`，不会启动 TUI。

> 协议与真实编辑器客户端（如 Zed）的交互一致；端到端一致性由
> [`util/acp-probe`](../util/acp-probe/main.go) 做回归验证。

## 快速开始

```bash
# 以 ACP Agent 模式启动，客户端以项目目录为工作目录通过 stdio 与之通信
go build -o bin/nekocode-tui ./cmd/tui
bin/nekocode-tui --acp
```

客户端提供的 stdio MCP Server 会启动本地进程，默认禁用。仅信任客户端及其
工作区配置时使用 `bin/nekocode-tui --acp --allow-client-mcp`。

需要客户端（编辑器）以当前项目目录作为子进程工作目录启动该二进制，并在
`session/new` 里传入与之一致的绝对 `cwd`。

## 公开 API

包对外只暴露一个最薄入口，其余均为协议内部实现：

- `RunStdio(ctx)` — 用 `runtime/standard` 的标准运行时，在 `os.Stdin`/`os.Stdout`
  上启动 ACP v1 Agent。`Serve(ctx, in, out, backend, cwd)` 是传输无关的核心，
  需要自行持有 `Backend` 生命周期，一般嵌入方用 `RunStdio` 即可。

## 支持的能力

通过 `initialize` 应答的 `agentCapabilities` 声明（见 [`types.go`](types.go)）：

| 能力 | 值 | 说明 |
| --- | --- | --- |
| `loadSession` | ✅ | 支持 `session/load` |
| `session.list` / `session.delete` | ✅ | `sessionCapabilities.list/delete` 已声明 |
| `promptCapabilities` | 全部 false | 不声明 image / audio / embeddedContext |
| `mcpCapabilities` | 全部 false | 仅 ACP v1 基线的 stdio MCP，不声明 http / sse |
| 会话配置项 | ✅ | `session/new` / `session/load` 响应携带 `configOptions`，支持 `session/set_config_option` |

提示内容（prompt content）只接受 `text` 与 `resource_link` 两种类型，
其他类型会以 `-32602` 拒绝（见 [`content.go`](content.go)）。

## 协议方法

### 服务端处理（客户端 → Agent）

在 [`server.go`](server.go) 的 `handle` 中分发：

| 方法 | 说明 |
| --- | --- |
| `initialize` | 握手，设置协议版本并返回能力与 Agent 信息（name: `nekocode`） |
| `session/new` | 新建会话，并把客户端传入的 stdio MCP Server 注册到后端 |
| `session/load` | 恢复会话；会把已持久化的历史重放成 `session/update` 通知，并在回放结束后按 session-usage RFD 立即上报一条 `usage_update`（当前上下文占用） |
| `session/list` | 列出当前工作目录下的会话，支持按 `cwd` 过滤 |
| `session/delete` | 删除当前工作目录下的会话 |
| `session/prompt` | 启动一轮运行，流式回推更新直到结束 |
| `session/set_config_option` | 会话配置：切换模型、思考深度（reasoning effort）、权限模式（full access） |
| `session/cancel` | 取消当前会话正在运行的那一轮 |

`session/set_config_option` 的三个配置项（见 [`config.go`](config.go)）：
`model`（select，可选值为配置的模型名）、`reasoning_effort`（select，含 `auto`，
仅在当前模型支持推理深度时出现，切换模型后选项随之变化）、`full_access`
（权限模式；客户端在 `initialize` 声明 `clientCapabilities.session.configOptions.boolean`
支持时为 boolean 开关，否则为 `manual` / `full` 的 select——不声明 boolean 的
客户端也能正常切换；按 session 隔离、仅在当前 ACP 连接内保存，不落盘）。设置时两种形式（boolean 值或
`manual` / `full` 字符串）均接受。配置变更在 prompt 运行中会被拒绝（`-32000`），
成功后响应返回刷新的完整 `configOptions`。

注意：`session/list` 默认只列出**当前 Agent 工作目录**下的会话，传 `cwd`
参数时可过滤其他目录；`session/load`、`session/delete` 与 `session/prompt`
会校验会话的 `cwd` 必须等于 Agent 工作目录。

### Agent → 客户端

- `session/update` 通知：流式推送 `agent_message_chunk`、`agent_thought_chunk`
  （推理过程）、`tool_call` / `tool_call_update`（状态 `pending` → `in_progress` →
  `completed` / `failed`；被权限规则拦截的工具没有前置的启动事件，会先补发
  `tool_call` 再发 `failed` 的 update）、`plan`、`usage_update`
  （上下文占用与窗口大小，供客户端显示 token 用量）等更新。
  `user_message_chunk` 仅在 `session/load` 历史回放时出现；
- `session/request_permission` 请求：当运行需要审批时发出，携带 `allow_once` /
  `allow_always`（仅项目级作用域）`/reject_once` 选项；
- 运行中遇到 `QuestionRequested`（提问）事件时，本实现会直接以「拒绝」应答，
  不向客户端转交提问能力。

### 未实现的方法

ACP v1 中其余方法（`session/resume`、`session/close`、`session/set_mode`、
`fs/read_text_file`、`fs/write_text_file`、
`terminal/*`、elicitation、`authenticate` / `logout`）均未声明对应能力，
按协议客户端应据 `agentCapabilities` 自行适配、不发起调用；若被调用统一返回
`-32601`。

### 错误码一览

| 错误码 | 触发条件 |
| --- | --- |
| `-32700` | 行不是合法 JSON |
| `-32600` | `jsonrpc` 版本不是 `2.0`；重复 `initialize` |
| `-32601` | 未知方法 / 未实现的方法 |
| `-32602` | 参数校验失败：未知 `sessionId`、`cwd` 不匹配、不支持的 MCP 类型或 prompt 内容类型等 |
| `-32603` | 后端运行错误（运行失败、事件流关闭等） |
| `-32002` | `initialize` 之前调用其他方法 |
| `-32000` | 有一轮 prompt 在跑时再新建/加载/删除会话，或并发 prompt |

## 约束与边界

- **单工作目录**：Agent 固定以进程启动时所在目录为工作目录；`session/*` 均校验
  `cwd` 为绝对路径且等于该目录。`additionalDirectories`（额外工作区）**不支持**，传入即报错。
- **MCP Server**：默认拒绝客户端进程启动；显式使用 `--allow-client-mcp` 后支持
  `session/new` / `session/load` 传入的 `stdio` 类型。每次最多 16 个、配置总计
  最多 64 KiB，并以原子集合替换；`http` / `sse` 会返回错误。
- **会话持久化时机**：会话在包含第一条消息后才会落盘（存储层设计：空会话不产生
  记录）。因此对刚创建的空会话调用 `session/load` / `session/delete` 会得到
  `-32602`；`session/prompt` 对未落盘的**当前活动会话**放行，首条消息正常发送。
- **单活动会话**：同一时刻只允许一轮 `session/prompt` 在跑；运行中再新建/加载/删除
  会话会返回 `-32000`。
- **图片重放上限**：`session/load` 重放历史图片时，单个文件超过
  `maxReplayImageBytes`（8 MiB）会被跳过，避免大图淹没客户端流（见 [`sessions.go`](sessions.go)）。
- **消息上限**：单条消息最大 1 MiB（`maxMessageSize`）；最多 64 个并发请求
  （`maxConcurrentRequests`），超限立即返回错误，reader 会继续处理审批响应和
  独立槽中的 `session/cancel`。
  请求 ID 以 `json.Number` 透传，超过 2^53 的大整数 ID 不会失真（见 [`codec.go`](codec.go)）。

## 目录 / 文件职责

| 文件 | 职责 |
| --- | --- |
| [`server.go`](server.go) | `server` / `Backend` 定义，`Serve` 装配与请求分发 |
| [`codec.go`](codec.go) | 换行分隔的 JSON-RPC 连接：请求/通知/响应的编解码与并发 |
| [`stdio.go`](stdio.go) | `RunStdio`：把标准运行时接到 stdin/stdout 上 |
| [`initialize.go`](initialize.go) | `initialize` 握手与能力声明 |
| [`types.go`](types.go) | 线协议类型与错误构造；`protocolVersion = 1` |
| [`sessions.go`](sessions.go) | 会话 new / load / list / delete，MCP Server 校验与历史重放 |
| [`prompt.go`](prompt.go) | `session/prompt`：启动运行并转发事件到 `session/update` |
| [`config.go`](config.go) | 会话配置项（模型/思考深度/权限模式）的构造与 `session/set_config_option` |
| [`events.go`](events.go) | 把后端运行时事件翻译成 ACP `session/update` 负载 |
| [`permission.go`](permission.go) | 审批视图 → `session/request_permission`，并把结果回写后端 |
| [`content.go`](content.go) | 校验/拼接 prompt 内容块 |
| [`cancel.go`](cancel.go) | `session/cancel` 取消正在运行的 turn |

## 后端抽象

`Backend`（见 [`server.go`](server.go)）是 ACP 层对 NekoCode 运行时的唯一依赖面，
覆盖运行启停、审批/提问决策、事件订阅、会话管理与 MCP 注册。
`controlruntime.Runtime` 实现了该接口（`server.go` 底部有编译期断言），因此 ACP 适配层
可以脱离具体运行时单独测试——`*_test.go` 即是围绕假后端做协议级验证。
