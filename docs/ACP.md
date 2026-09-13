# ACP（Agent Client Protocol）

本文档讲述 ACP v1 的协议标准、拓展性设计，以及 NekoCode 的实现现状。
包内部的实现细节（文件职责、边界行为、错误码全集）见 [`acp/README.md`](../acp/README.md)，
端到端测试工具见 [`util/acp-probe`](../util/acp-probe/main.go)。

## 什么是 ACP

ACP（Agent Client Protocol）是由 Zed 团队发起的开放协议，定义**编辑器（Client）与
AI 编程 Agent（Agent）**之间的通信规范，作用类似 LSP 之于语言服务器：一次接入，
处处可用。协议于 2025 年 9 月发布 v1（当前稳定版），通过 [RFD 流程](https://agentclientprotocol.com/rfds)
持续增量演进；2026 年 7 月发布 v2 **草案**（尚未稳定，主流实现均为 v1）。

主流接入方：Zed（客户端）、Claude Code、Codex、Gemini CLI、OpenCode、
Copilot、Cursor、Pi 等（Agent 侧）。

## 协议基础

### 传输与编码

- 一条 **stdio** 管道，**换行分隔的 JSON-RPC 2.0**（NDJSON）
- 客户端以项目目录为工作目录启动 Agent 进程
- `initialize` 必须是首个请求；Agent 在 `session/new` 中校验 `cwd`
  必须等于自己的工作目录

### 核心设计：能力协商

一切可选项遵循同一条规则——**先声明，后使用**：

- Agent 在 `initialize` 应答中声明 `agentCapabilities`：
  `loadSession`、`promptCapabilities`（image / audio / embeddedContext）、
  `mcpCapabilities`（http / sse）、`sessionCapabilities`（list / delete /
  additionalDirectories / resume / close）、`auth`
- 客户端在 `initialize` 请求中声明 `clientCapabilities`：
  `fs`（readTextFile / writeTextFile）、`terminal`、
  `session.configOptions.boolean`、`elicitation`、`auth.terminal`
- 未声明的能力，对端不应发起调用；协议类型对未知字段宽容
  （`x-deserialize-default-on-error`），保证前向兼容

一个典型例子：boolean 类型的会话配置项必须等客户端声明
`clientCapabilities.session.configOptions.boolean` 后 Agent 才能下发，
否则应使用所有客户端都支持的 select 形态。

## v1 方法全景

### 客户端 → Agent

| 分组 | 方法 | 说明 | 依赖能力 |
| --- | --- | --- | --- |
| 握手 | `initialize` | 双向能力声明，返回 Agent 信息 | — |
| 会话 | `session/new` | 创建会话（`cwd` + `mcpServers` 必填） | — |
| 会话 | `session/load` | 恢复历史并回放 `session/update` | `loadSession` |
| 会话 | `session/list` | 列出会话（cwd 过滤 + cursor 分页） | `sessionCapabilities.list` |
| 会话 | `session/delete` | 删除会话 | `sessionCapabilities.delete` |
| 会话 | `session/resume` | 无回放的轻量恢复 | `sessionCapabilities.resume` |
| 会话 | `session/close` | 显式关闭 | `sessionCapabilities.close` |
| 对话 | `session/prompt` | 发送消息；基线内容类型 `text` / `resource_link` | — |
| 对话 | `session/cancel` | 取消当前轮（通知语义，无响应） | — |
| 配置 | `session/set_config_option` | 会话配置项写入（select / boolean） | — |
| 配置 | `session/set_mode` | 会话模式切换 | — |
| 鉴权 | `authenticate` / `logout` | 配合 `authMethods` | `auth` |

### Agent → 客户端

| 方法 | 方向 | 说明 |
| --- | --- | --- |
| `session/update` | 通知 | 核心流：`user/agent_message_chunk`、`agent_thought_chunk`、`tool_call` / `tool_call_update`、`plan`、`config_option_update`、`usage_update` 等 |
| `session/request_permission` | 请求 | 工具审批；客户端取消 turn 时必须以 `Cancelled` 应答 |
| `elicitation/create` | 请求 | 向用户收集结构化输入（JSON Schema 表单 / URL 流程） |
| `fs/read_text_file`、`fs/write_text_file` | 请求 | Agent 借用客户端文件系统 | 
| `terminal/create`、`output`、`release`、`wait_for_exit`、`kill` | 请求 | Agent 借用客户端终端 |

### MCP 接入

`session/new` / `session/load` 的 `mcpServers` 支持三种形态：

- **stdio**（`command` / `args` / `env`）：**v1 基线，所有 Agent 必须支持**
- **http** / **sse**（`url` / `headers`）：需 Agent 在 `mcpCapabilities` 中声明

### Turn 生命周期

v1 隐含"事件流发生在一个用户发起的 turn 内"：`session/prompt` 请求 ↔ 响应
包裹整个工作过程，响应的 `stopReason`（`end_turn` / `cancelled` / `refusal` /
`max_tokens`）标志轮次结束。客户端发出 `session/cancel` 后仍须继续接受
`tool_call_update`（Agent 可能发送最终更新后才返回取消原因）。

## 拓展性设计

由浅入深四条路径：

1. **会话配置项**（客户端零适配）：`configOptions` 声明 select / boolean 项，
   客户端自动渲染下拉/开关，`session/set_config_option` 写回。适合一切
   会话级开关。
2. **`_meta` 字段**：每个消息都有协议保留的 `_meta`，实现方不得对其值做
   假设——可安全携带自定义元数据。
3. **扩展方法 / 通知**（`ExtRequest` / `ExtNotification`）：规范外的 method
   名即是合法扩展，未知方按 JSON-RPC 语义忽略。
4. **打开可选能力**：在 `agentCapabilities` 中声明开关并实现对应方法。
   注意 `SessionUpdate` 变体名是规范枚举，不能自创（客户端会静默丢弃
   未知变体）；扩展 update 内容只能走 `_meta` 或 Ext 通知。

## 版本现状

| 版本 | 状态 | 说明 |
| --- | --- | --- |
| v1 | **稳定，生态主流** | 2025-09 发布；schema 见 [GitHub releases](https://github.com/agentclientprotocol/agent-client-protocol) |
| v2 | Draft | 2026-07-20 发布草案；主要变化：超越 turn 的生命周期（update 可随时发、prompt 响应仅表示确认、idle 指示）、消息/工具调用统一 patch 语义（稳定 ID + 增量更新）、结构化 Diff、权限请求解耦、`_` 前缀私有枚举。官方建议挂在版本协商 + feature flag 后，稳定前勿在生产默认启用 |

## NekoCode 实现

NekoCode 以 `nekocode-tui --acp` 启动即为 ACP v1 Agent（`acp.RunStdio`）。
`acp` 包通过 `Backend` 接口对接 `runtime`，与 TUI / HTTP 等交互面共享同一运行时。
客户端提供的 stdio MCP 会启动本地进程，因此默认拒绝；只有明确信任客户端及其
工作区配置时才使用 `--allow-client-mcp` 开启。

### 能力声明

| 能力 | 值 |
| --- | --- |
| `loadSession` | ✅ |
| `sessionCapabilities.list` / `delete` | ✅ |
| `promptCapabilities`（image / audio / embeddedContext） | 全部 false |
| `mcpCapabilities`（http / sse） | 全部 false（stdio 为基线，无需声明） |

### 已实现

| 方法 | NekoCode 行为 |
| --- | --- |
| `initialize` | 协议版本 1；记录客户端 boolean 配置项支持情况 |
| `session/new` | 新建会话；显式允许时原子替换客户端传入的 **stdio MCP Server**；响应携带 `configOptions` |
| `session/load` | 原子替换 MCP Server + 历史回放（推理过程、工具调用、图片，8 MiB 图片上限）+ 回放结束即上报 `usage_update` + `configOptions` |
| `session/list` / `session/delete` | 限定本工作目录的会话 |
| `session/prompt` | 流式回推 `agent_message_chunk` / `agent_thought_chunk` / `tool_call` / `tool_call_update` / `plan` / `usage_update`（上下文占用与窗口大小）；被拦截工具先补发 `tool_call` 再发 `failed` update |
| `session/cancel` | 独立并发槽，turn 运行中可及时取消 |
| `session/set_config_option` | `model`（select）、`reasoning_effort`（select，含 `auto`，随模型变化）、`full_access`（权限模式；客户端声明 boolean 能力时为开关，否则为 `manual`/`full` 下拉） |
| `session/request_permission` | 选项 `allow_once` / `allow_always`（仅项目级）/ `reject_once`，按工具调用 ID 精确关联 |
| stdio MCP | 完整链路：spawn → MCP 握手 → tools/list → 工具经 capability proxy 进入模型 |

### 行为要点

- **错误码全集**：`-32700` / `-32600` / `-32601` / `-32602` / `-32603` / `-32002` / `-32000`（详见 `acp/README.md`）
- **大整数请求 ID** 以 `json.Number` 透传，2^53 以上不失真
- **会话持久化时机**：会话包含首条消息后才落盘，空会话不可 load / delete，但当前活动会话的 prompt 不受影响
- **并发**：同一时刻仅一轮 prompt；运行中变更会话/配置返回 `-32000`
- **会话配置**：模型、思考深度和权限模式按 ACP session 隔离，仅在当前连接内保存，
  不写入全局配置；连接关闭后恢复启动时配置
- **客户端 MCP 安全边界**：默认禁用；`--allow-client-mcp` 显式开启后，每次最多
  16 个 server、配置总计最多 64 KiB，替换失败保留当前集合
- **测试**：`acp` 包单元测试（假后端）+ `util/acp-probe` 端到端一致性回归
  （spawn 真实进程，覆盖全部方法、错误码、MCP 握手、真实模型对话与回放）

### 未实现

| 项 | 依赖 | 备注 |
| --- | --- | --- |
| `session/resume` / `session/close` | `sessionCapabilities` | 轻量恢复 / 显式关闭 |
| `promptCapabilities.image` | — | 粘贴图片对话；后端已具备图片处理，建议优先实现 |
| `fs/read_text_file` / `fs/write_text_file` | 客户端声明 `fs.*` | Agent 借用客户端文件系统（远程开发场景） |
| `elicitation/create` | 客户端声明 `elicitation` | 结构化用户输入；可替代当前"直接拒绝 Question 事件"的策略 |
| `terminal/*` | 客户端声明 `terminal` | Agent 借用客户端终端 |
| `session/set_mode` + `modes` | — | 会话模式 |
| `mcpCapabilities.http` / `sse` | — | 远程 MCP Server，需在 `bot/extension/mcp` 增加 HTTP 传输 |
| `additionalDirectories` | `sessionCapabilities.additionalDirectories` | 多根工作区 |
| 鉴权（`authenticate` / `logout` / `authMethods`） | `auth` | 本地 Agent 场景暂无需求 |

### 升级到 v2 的展望

v2 稳定前不建议跟进。稳定后主要迁移点：prompt 生命周期模型
（`acp/prompt.go` 的 turn 假设）、事件 patch 语义（`acp/events.go`）。
版本协商点已就位（`initialize` 的 `protocolVersion`）。
