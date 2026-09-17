# NekoCode Headless 协议 v2

NekoCode 提供 `nekocode-headless/2` 双向 NDJSON 协议，适合由宿主启动 CLI
子进程进行多轮对话、观察工具执行、回答审批和问题，以及取消运行。

这是 NekoCode 自有协议，基于 runtime 的真实能力定义控制方法、事件和终态。
NDJSON 是传输格式；协议版本和 capability 列表才是接入契约。

## 启动

使用现有 NekoCode 模型与权限配置，以项目目录为工作目录启动：

```bash
# 单次 prompt，result 后退出
nekocode-tui -p "解释这个项目" --output-format stream-json

# 从 stdin 读取一个文本 prompt，EOF 后执行
printf '解释这个项目' | nekocode-tui -p --output-format stream-json

# 双向多轮连接
nekocode-tui --headless

# 等价的显式格式参数，并开启增量预览
nekocode-tui -p --input-format stream-json --output-format stream-json \
  --include-partial-messages --verbose

# 恢复已有会话
nekocode-tui --headless --resume SESSION_ID
```

`--headless` 同时选择 stream-json 输入和输出；不能同时传 prompt 参数。
`--stream-json` 是保留的等价入口；`--verbose` 目前不改变输出。`--acp` 仍使用独立的 ACP 实现。

stdout 只输出协议 JSON；CLI 诊断错误写 stderr，内部诊断日志沿用 NekoCode 配置。
每个 JSON 对象占一行；支持任意输入分块和没有末尾换行的最后一行。

## 会话和运行生命周期

- 一个连接独占一个 runtime，默认创建新会话；`--resume` 在开始读取消息前恢复会话。
- `initialize` 可省略；如果使用，必须在第一条 user 消息之前发送，且只能成功一次。
- user 消息按 FIFO 排队，一个 runtime 同时只运行一个 turn。
- 每次接受输入后输出 `system/input_queued`，其 `accepted_uuid` 是关联 ID。
- 每个已启动 turn 输出 `system/init`，最终恰好一条 `result`。
- `result` 之前等待 runtime 清理完成，随后才启动下一轮。
- stdin 保持打开时，空闲进程继续等待下一轮输入。
- EOF 后排空已接受的队列，未回答的审批与问题自动拒绝；之后产生的交互也自动拒绝。
  需要交互的宿主应保持 stdin 打开。
- SIGINT、SIGTERM、输出失败或可靠事件订阅异常关闭时，取消当前运行并清理连接。
- 连接级故障以 `system/error`、`fatal:true` 尽力报告并返回非零退出状态。
  管道已经损坏时不保证发出错误帧或 result；宿主必须处理进程退出。
- 会话切换通过空闲时的 `session.new/resume` 控制请求进行；运行内绕过该入口切换会话会导致连接报错退出。
- 运行事件（包括 `system/init`）携带 `run_id` 和 `user_message_uuid`；启动失败而未分配运行 ID 时省略 `run_id`。控制响应通过 `request_id` 关联。

### 输入

```json
{"type":"user","uuid":"u-1","message":{"role":"user","content":"解释这个项目"}}
```

content 可为字符串或仅含 `{"type":"text","text":"..."}` 的数组。
空输入、图像/文档/外部 tool_result、非 null 的 `parent_tool_use_id`（包括空字符串）、`shouldQuery:false`，
以及除 `next` 外的 priority 均明确拒绝。省略 priority 等同 FIFO。

省略 uuid 时由服务端分配。相同 uuid 在同一连接内只接受一次；重复输入输出
`system/input_duplicate`，其中 `user_message_uuid` 指向重复的输入，不重复运行。
传入 session_id 必须与当前绑定会话一致，不能用它切换会话。

输入拒绝输出非 fatal `system/error`，通常附带 `rejected_uuid`，不产生 result。
输入关联 ID 与每条输出帧的 uuid 是不同概念。

### 完整消息与增量

```json
{"type":"assistant","session_id":"...","uuid":"...","user_message_uuid":"u-1","parent_tool_use_id":null,"message":{"id":"...","role":"assistant","model":"...","content":[{"type":"text","text":"项目说明"}]}}
```

完整 assistant 块支持 `text`、`thinking`、`tool_use`。一个 frame 含一个块。
模型消息 ID 由适配层生成；完整文本边界来自 runtime `assistant_message`，
其余连续文本/思考在类型切换、工具、交互或 turn 终态处完成。

开启 `--include-partial-messages` 后还会输出 `stream_event`，含
`content_block_start`、`content_block_delta`、`content_block_stop`。
每个独立块 index 为 0，通过顶层 `message_id` 关联到完整 assistant 的 `message.id`。

这些是**合成的内容块事件**，不包含 Anthropic 原始 `message_start/message_stop`、
thinking signature 或供应商 usage。完整 assistant 内容是权威值，可替换增量预览；
不要把二者重复追加到界面。`result.result` 是最终答案，也不应重复追加。

同一模型响应交错输出 text/thinking 时，完整文本会扣除已经提交的文本前缀。
如果模型修正了此前输出，服务端会用原 `message.id` 发送空文本撤回旧块，再发送修正内容。
宿主应按 `message.id` 替换完整块，而不是将每条 assistant 无条件追加。

### 工具

```json
{"type":"assistant","message":{"id":"...","role":"assistant","model":"...","content":[{"type":"tool_use","id":"tool-1","name":"shell","input":{"command":"go test ./..."}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-1","content":"ok","is_error":false}]}}
```

示例省略通用 envelope 字段。工具名保持 NekoCode 原名，结构化 input 保留参数类型。
权限被拒绝的工具也输出配对的 tool_use/tool_result。运行终止时，对已开始但尚未完成的
工具补发 `is_error:true` 的结果，避免宿主留下悬空调用。

子代理工具带 `nekocode_subagent_id` 扩展字段；目前不伪造 `parent_tool_use_id`。
工具 wire ID 在当前 turn 内唯一；模型重复使用调用 ID，或不同子代理使用相同 ID 时，
适配层为后续调用分配新 ID，工具结果和审批请求使用对应的 wire ID。
子代理身份同时传递给审批 broker，避免权限请求关联到另一个子代理的同名调用。
结构化参数保持原始 JSON 数字精度。

### 结果与统计

`result.subtype` 支持 `success`、`error_during_execution`、`error_cancelled`、
`error_step_limit`，含 `is_error`、`result`、`duration_ms` 和可选 `errors` 数组。
`stop_reason` 分别为 `completed`、`execution_error`、`cancelled`、`step_limit`。
runtime 的失败/取消终态优先于代理摘要，保存会话等收尾失败不会被标为成功。

代理提供摘要时附带真实 `step_count`（主代理执行步数）；命令或提前终止未提供摘要时省略，
不填假零值。`permission_denials` 汇总本轮审批 broker 的拒绝/过期记录，字段为
`approval_id/tool_name/call_id/subagent_id/status`；不声称覆盖所有工具内部的策略拒绝。

有运行指标时附带 `nekocode_usage`，其字段是 runtime Metrics 原样快照；Turn 字段表示
runtime 的运行统计，Context 字段表示上下文占用。它不是 Anthropic usage，不能直接
当成 Claude SDK 的累计费用。未提供 `total_cost_usd`、`modelUsage`、`duration_api_ms`
或 `num_turns`，不会用零值伪造这些数据。

## 双向控制

### 初始化与能力发现

```json
{"type":"control_request","request_id":"init-1","request":{"subtype":"initialize","protocol_version":"nekocode-headless/2"}}
```

成功响应的 `response.response` 包含 `protocol_version`、`capabilities` 和 `server`。
版本可省略；显式传入不支持的版本会返回关联的 control error，可以修正后重试。
`server` 包含 cwd、session_id、model、permission_mode、实际支持的 tools/commands。
每轮 `system/init` 也携带此元数据。工具清单是配置的工具目录，具体调用仍需通过权限检查。

所有控制请求统一使用 `request.subtype`，成功数据位于 `response.response`，
错误位于 `response.error`，均回显 `request_id`。以返回的 capabilities 判断后端可用能力。

`capabilities` 同时包含功能标识和控制方法名。功能标识如 `text`、
`subagent_output`、`run_summary`、`permission_denials` 不能作为控制方法调用；
调用它们会返回 control error。可调用的方法见下表，以及后文的初始化、中断和交互说明。

对于嵌入式后端，能力声明逐项对应 runtime 服务：

| runtime 能力标志 | headless 控制方法 |
|---|---|
| `ModelCatalog` | `models.list`；仅有当前模型查询不声明此方法 |
| `ModelSelection` | `model.set` |
| `PermissionControl` | `permissions.set` |
| `Sessions` | `sessions.list`、`session.history` |
| `SessionCreate` / `SessionResume` / `SessionDelete` | 分别对应 `session.new` / `session.resume` / `session.delete` |
| `Extensions` | `extensions.list` |
| `Checkpoints` | `workspace.checkpoints` |
| `Rewind` | `workspace.rewind` |
| `Steering` | `run.steer` |

标准 runtime 根据已配置服务填充这些标志；自定义 Management 后端须如实声明。
检查点查询直接读取结构化数据，不解析命令菜单；读取失败返回 control error，
只有成功查询且无检查点时才返回空数组。查询与回滚能力分别声明。
`server.info` 和 `shutdown` 由适配层提供。


| 方法 | 请求字段 | 成功数据 / 约束 |
|---|---|---|
| `server.info` | 无 | 当前元数据与能力 |
| `models.list` | 无 | models、active |
| `model.set` | model | 当前会话模型选择；空闲时，不写全局默认配置 |
| `permissions.set` | full_access: boolean | permission_mode；空闲时 |
| `sessions.list` | 无 | sessions |
| `session.history` | 可选 session_id、offset、limit | 当前会话 messages、total、next_offset、has_more；空闲时 |
| `session.new` | 无 | 新会话元数据；空闲时 |
| `session.resume` | session_id | session_id；空闲时 |
| `session.delete` | session_id | deleted；空闲时，不能删除当前会话 |
| `extensions.list` | 无 | skills、plugins、mcp_servers 的只读摘要 |
| `workspace.checkpoints` | 无 | checkpoints（id、label、description）；空闲时 |
| `workspace.rewind` | checkpoint_id | message、workspace；空闲时，按 NekoCode 检查点恢复 |
| `run.steer` | text | accepted、run_id；只允许运行中追加引导 |
| `shutdown` | mode: drain / cancel | state、cancelled_message_uuids |

空闲要求当前运行和输入队列均已清空，关闭过程中拒绝状态修改。
会话绑定变化时发送 `system/session_changed`。恢复操作若部分完成后失败，也会同步实际
绑定并报告原错误；宿主应以该事件或 `server.info` 的 session_id 为准。

历史 offset 从 0 开始，limit 默认 100，允许 1..1000。按 next_offset 请求下一页；
会话内容变化后应重新从 0 读取。过大响应返回 control error，连接继续可用，可降低 limit。
单条消息超过单帧上限时不支持通过此接口读取。扩展摘要不返回进程参数、环境变量或凭据。

`run.steer` 的 ACK 表示 runtime 接受引导，不保证模型已消费；
`system/input_accepted` 的 input 携带来源，Source ID 对应引导的 request_id。

`shutdown/drain` 停止接受新输入，排空已接收的 turn，仍允许宿主回答审批与问题。
`shutdown/cancel` 取消当前运行、丢弃排队输入并拒绝待答交互；队列中尚未投递的旧交互也不会再发给宿主。
ACK 后继续读取当前 turn 的 result；清理和输出排空后进程退出，无需关闭 stdin。
drain 可能等待宿主交互；需要立即取消时可再次发送 cancel。

不支持宿主注入 Hooks、SDK-hosted MCP、agents、skills、system prompt、JSON Schema
或 prompt suggestions；这些字段携带非空配置时返回 control error。
未知字段忽略，未知 control subtype 返回 error，不悬挂请求。

### 审批

```json
{"type":"control_request","request_id":"nc-1","request":{"subtype":"can_use_tool","tool_name":"shell","tool_use_id":"tool-1","input":{"command":"go test ./..."},"nekocode_approval":{}}}
{"type":"control_response","response":{"subtype":"success","request_id":"nc-1","response":{"behavior":"allow"}}}
```

`behavior` 只接受 allow 或 deny；allow 只批准当前请求，不持久化权限规则。
`nekocode_approval` 携带 runtime 的风险、权限范围和路径等审批上下文，可能为 null。
审批 `input._preview` 可携带字符串 diff/命令预览；它属于展示元数据，不进入工具输入或
ArgsHash/ToolCallHash 的参数哈希。运行时回调 `_sub_callback` 不进入审批请求。
`updatedInput` 如提供必须是对象，比较时双方均排除 `_preview`、`_sub_callback`，
因此可回显完整审批 input，也可仅回显 tool_use.input。被排除的字段不会用于替换原展示或执行数据；
其他参数（包括合法的下划线字段）必须保持相同。参数替换、非空 `updatedPermissions` 均拒绝，
同时拒绝该审批并输出 `system/error`。错误或无效回复也按拒绝处理。

### 提问（NekoCode 扩展）

```json
{"type":"control_request","request_id":"nc-2","request":{"subtype":"nekocode_question","questions":[{"question":"继续吗？","options":[],"custom":true}]}}
{"type":"control_response","response":{"subtype":"success","request_id":"nc-2","response":{"answers":[["继续"]]}}}
```

回答使用与 questions 顺序对应的二维字符串数组；拒绝使用 `{"rejected":true}`。

### 中断

```json
{"type":"control_request","request_id":"stop-1","request":{"subtype":"interrupt","cancel_queued":true}}
```

成功 ACK 中有 `queued_message_uuids` 与 `cancelled_message_uuids` 数组。
取消队列中的输入不会启动，不产生 result；当前运行随后产生自己的 result。
ACK 不代表运行已经清理完成，必须继续读取 result。
若后端拒绝取消，返回 control error，并保留原排队输入，不提前清空队列。

审批/问题被运行取消或 EOF 作废时，服务端发送 `control_cancel_request`。
晚到或重复的 control_response 被忽略。宿主发送的 control_cancel_request 不会批准或
回答服务端发起的交互。`keep_alive` 可随时发送，服务端忽略它。

## 扩展事件与暂未支持的能力

`system/nekocode_event` 携带 `event_type` 与 `payload`，转发工具预览、phase、todos、
子代理开始/结束事件。子代理结束仅表示生命周期结束，不保证成功。

子代理模型输出使用独立 `subagent` 帧，携带 `subagent_id`、`run_id`、
`user_message_uuid`、`event`、`text`。`event` 为 text_delta、reasoning_delta 或 message。
增量仅在开启 partial messages 时发送；message 只携带该子代理本次模型响应的非空完整文本。
无文本的步骤不发送 message，包括仅有工具调用或思考内容的步骤；message 不是每步结束标记。
宿主按子代理维护文本预览，收到 message 后用完整文本替换本次文本预览，不要重复追加增量。
reasoning_delta 是独立的思考预览，不保证后续有 message 与之配对，可在子代理结束时结算。
子代理消息不会合入主代理最终答案。

统一后台任务进度/终态/独立取消、队列优先级、连接级工具限制和可配置运行预算仍未暴露。
运行中切换模型或会话也不支持，须等待空闲。

已确认的核心范围已实现；剩余独立阶段和暂缓项见 [STREAM_JSON_GAPS.md](STREAM_JSON_GAPS.md)。

## 限额与可靠性

- 单帧输入和输出上限约 8 MiB（输入计入换行开销）；连续 assistant 聚合块上限 4 MiB。
- 输入 UUID、session ID 和控制 request ID 上限 256 字节。
- 最多 32 个排队 turn，排队正文合计最多 16 MiB；每个连接最多保留 10000 个输入 ID。
- 输出队列最多 128 帧、合计 16 MiB。输出拥堵会明确终止连接，不静默丢帧。
- runtime 使用 `EventFilter{Reliable:true}` 订阅，订阅积压超限后关闭；适配层将其视为
  连接失败并取消运行。默认 GUI/ACP 等订阅的原有丢弃策略不受影响。
- 部分写入或输出错误属于连接故障；正常退出前会等待输出队列排空。

Go 宿主可调用 `headless.Serve`，传入独占使用的 Backend 与可关闭流。
Serve 负责关闭输入/输出，流的 Close 必须能解除正在阻塞的 Read/Write；Backend 生命周期
由调用方管理。Backend 的 Events 必须遵守 Reliable 语义。

## 验证

```bash
go test -race ./headless ./runtime/... ./acp ./bot/agent/... ./bot/core ./bot/extension/tool/runtime/core ./cmd/tui
```

测试使用真实 runtime 和可控 Runner，验证 NDJSON 分块、双向审批/问题、多轮排队、
EOF、重复 ID、未知请求、中断工具结算、结构化参数、超大输入和慢消费者。
管理回归测试还覆盖逐项能力声明、分页历史、版本检查、会话绑定、终态优先级、
运行 ID 关联、关闭后的过期交互，以及中断失败时保留队列。
另含真实子进程测试：stdin 保持打开、取消、stdout 背压，以及 Unix 标准流阻塞模式恢复。
测试使用本地可控模型和 Runner，未进行外部模型服务端到端验证。
