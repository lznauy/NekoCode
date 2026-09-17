# headless

`headless` 是 NekoCode 的**无界面接入层**：把 NekoCode 的运行时能力（多轮对话、工具执行、
审批、提问、取消）通过一条标准的 **NDJSON over stdio** 双向协议暴露给宿主程序，例如
编辑器插件、CI 脚本、IM/机器人后端或自研客户端。

- 协议版本：`nekocode-headless/2`（NekoCode 自有的双向 NDJSON 协议）。
- 完整线协议契约见 [`docs/STREAM_JSON.md`](../docs/STREAM_JSON.md)。
- 本 README 面向**接入开发**：怎么起、怎么发、怎么收、怎么处理审批与错误。

> 本包当前是仓库内部包（`module nekocode`），第三方主要应通过**子进程**方式接入；
> Go 侧 `Serve` 接口面向仓库内/嵌入式宿主（见 [Go 嵌入](#go-嵌入)）。

---

## 两种接入方式

| 方式 | 适用 | 入口 |
|------|------|------|
| 子进程（推荐） | 任意语言的第三方宿主 | `nekocode-tui --headless` 等 CLI 参数，走 stdin/stdout |
| Go 嵌入 | 仓库内或可编译进同一二进制的宿主 | `headless.Serve` / `headless.RunStdio` + 实现 `Backend` |

---

## 快速开始（子进程）

### 启动

```bash
# 单次 prompt：产出 result 后进程自行退出
nekocode-tui -p "解释这个项目" --output-format stream-json

# 单次 prompt，prompt 从 stdin 读（EOF 后执行）
printf '解释这个项目' | nekocode-tui -p --output-format stream-json

# 双向多轮长连接：stdin 保持打开即可持续对话
nekocode-tui --headless

# 开启增量预览（stream_event）与显式格式参数
nekocode-tui -p --input-format stream-json --output-format stream-json \
  --include-partial-messages --verbose

# 恢复已有会话
nekocode-tui --headless --resume SESSION_ID
```

支持的参数：

| 参数 | 含义 |
|------|------|
| `--headless` / `--stream-json` | 同时选择 stream-json 输入与输出（不能与 prompt 参数同用） |
| `-p` / `--print [PROMPT]` | 单次 prompt；省略值时从 stdin 读一段文本 |
| `--input-format text\|stream-json` | 输入格式，默认 `text` |
| `--output-format stream-json` | 输出格式，headless 模式**必须**为 `stream-json` |
| `--resume SESSION_ID` | 在开始读取消息前恢复会话 |
| `--include-partial-messages` | 额外输出合成的 `stream_event` 增量块 |
| `--verbose` | 兼容常见启动外形，当前不改变输出 |

说明：

- 进入 headless 模式后只解析上述参数；出现其他参数会直接报错，不会静默忽略。未传 headless
  相关参数时 `nekocode-tui` 仍进入原 TUI（此时不解析这些参数），`--acp` 仍走独立的 ACP 实现。
- **stdout 只输出协议 JSON**；诊断错误走 stderr。每个 JSON 对象占一行（NDJSON），
  支持任意输入分块，最后一行可以没有换行符。
- 需要交互（审批/提问）的宿主必须保持 stdin 打开；stdin EOF 后未回答的交互会被自动拒绝。

### 最小会话

一次 turn 的典型帧序列（节选，省略部分字段与能力，`user_message_uuid` 用于把输出帧关联回触发它的输入）：

```
→ {"type":"control_request","request_id":"init-1","request":{"subtype":"initialize"}}
← {"type":"control_response","response":{"subtype":"success","request_id":"init-1","response":{"protocol_version":"nekocode-headless/2","capabilities":["text","fifo_turns","interrupt","cancel_queued","can_use_tool","nekocode_question","synthetic_content_stream"]}}}
→ {"type":"user","uuid":"u-1","message":{"role":"user","content":"Say hello."}}
← {"type":"system","subtype":"input_queued","accepted_uuid":"u-1","session_id":"...","uuid":"..."}
← {"type":"system","subtype":"init","user_message_uuid":"u-1","protocol_version":"nekocode-headless/2","nekocode_version":"...","cwd":"/path/to/project","model":"...","permissionMode":"manual","capabilities":[...],"uuid":"..."}
← {"type":"system","subtype":"nekocode_event","user_message_uuid":"u-1","event_type":"phase_changed","payload":{"phase":"Reasoning"}}
← {"type":"assistant","user_message_uuid":"u-1","parent_tool_use_id":null,"message":{"id":"...","role":"assistant","model":"...","content":[{"type":"text","text":"Hello."}]}}
← {"type":"result","user_message_uuid":"u-1","subtype":"success","is_error":false,"result":"Hello.","duration_ms":28,"nekocode_usage":{...}}
```

`initialize` 可省略；一旦使用，必须在第一条 `user` 消息之前、且只能成功一次。

### 最小客户端（Python）

```python
import json, subprocess

proc = subprocess.Popen(["nekocode-tui", "--headless"],
                        stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True, bufsize=1)

def send(obj):
    proc.stdin.write(json.dumps(obj) + "\n"); proc.stdin.flush()

send({"type": "control_request", "request_id": "init-1",
      "request": {"subtype": "initialize"}})
send({"type": "user", "uuid": "u-1",
      "message": {"role": "user", "content": "解释这个项目"}})

for line in proc.stdout:                       # 每行一个 JSON 帧
    frame = json.loads(line)
    if frame["type"] == "control_request":     # 反向请求：必须作答，否则运行会阻塞
        req = frame["request"]
        if req["subtype"] == "can_use_tool":
            send({"type": "control_response", "response": {
                "subtype": "success", "request_id": frame["request_id"],
                "response": {"behavior": "allow"}}})
        elif req["subtype"] == "nekocode_question":
            send({"type": "control_response", "response": {
                "subtype": "success", "request_id": frame["request_id"],
                "response": {"answers": [["是"]]}}})
    elif frame["type"] == "result":            # 一个 turn 恰好一条 result
        print(frame["result"])
        break
```

---

## 消息流

- 一个连接独占一个 runtime，默认新建会话；`--resume` 在读取消息前恢复会话。
- `user` 消息按 FIFO 排队，同一时刻只运行一个 turn；每次接受输入都会回
  `system/input_queued`（关联 ID 是 `accepted_uuid`）。
- 每个已启动的 turn 输出一条 `system/init`，并在清理完成后输出**恰好一条** `result`；
  下一轮在 `result` 之后才开始。
- 空闲且 stdin 打开时进程继续等待新输入。
- 运行内绕过管理入口切换会话（例如 `/sessions`）会报错退出；正常切换应等待运行和队列空闲，再调用 `session.new` / `session.resume`。

## 帧速查

### 宿主 → 服务端

| `type` | 作用 | 关键字段 |
|--------|------|----------|
| `user` | 提交一轮输入 | `uuid`（可省略，服务端分配；同一连接内不可重复）、`message.content`（字符串或纯 text 块数组）、`session_id`（若给出必须与当前会话一致） |
| `control_request` | 控制指令 | `request_id` + `request.subtype`：`initialize`、`interrupt` 及下文 v2 管理方法 |
| `control_response` | 回答服务端发起的交互 | `response.request_id`、`response.subtype`、`response.response` |
| `control_cancel_request` | 撤销宿主自己的请求 | 服务端忽略（不会批准/回答服务端发起的交互） |
| `keep_alive` | 保活 | 服务端忽略 |

被拒绝的输入回**非 fatal** 的 `system/error`（通常带 `rejected_uuid`），不产生 `result`。
`message.content` 只接受字符串或**纯 `text` 块**数组；图像/文档/外部 `tool_result`、非 null 的
`parent_tool_use_id`（包括空字符串）、`shouldQuery:false`、`priority` 非 `next` 都会被明确拒绝。

### 服务端 → 宿主

| `type` | `subtype` / 内容 | 说明 |
|--------|------------------|------|
| `system` | `init` | 每轮开始：协议版本、NekoCode 版本、cwd、model、permissionMode、capabilities |
| `system` | `input_queued` | 输入已接受，关联 ID 为 `accepted_uuid` |
| `system` | `input_duplicate` | 重复 `uuid`，`user_message_uuid` 指向重复的输入，不重复运行 |
| `system` | `error` | 输入错误（非 fatal）或连接故障（`fatal:true`） |
| `system` | `session_changed` | 空闲时创建或恢复会话后的实际绑定 |
| `system` | `input_accepted` | runtime 已接受输入或引导；引导来源 ID 为控制请求的 `request_id` |
| `system` | `message` | 系统消息（如命令输出） |
| `system` | `nekocode_event` | 扩展事件：`event_type` + `payload`（phase、todos、子代理开始/结束、工具预览） |
| `assistant` | `message.content` | 完整块：`text` / `thinking` / `tool_use`，一个帧含一个块 |
| `user` | `message.content` | `tool_result`（与 `tool_use` 使用同一 `tool_use_id`） |
| `stream_event` | `content_block_start/delta/stop` | 仅 `--include-partial-messages` 时输出，**合成**增量块 |
| `subagent` | `text_delta` / `reasoning_delta` / `message` | `event` 区分输出类型；携带 `subagent_id`，增量仅在开启 partial messages 时发送 |
| `result` | `success` / `error_during_execution` / `error_cancelled` / `error_step_limit` | 每轮恰好一条；含 `is_error`、`result`、`duration_ms`、可选 `errors`、`nekocode_usage` |
| `control_request` | `can_use_tool` / `nekocode_question` | 反向请求，必须用 `control_response` 作答 |
| `control_response` | — | 对宿主 `control_request` 的应答 |
| `control_cancel_request` | — | 审批/提问因运行取消或 EOF 作废 |

`user_message_uuid` 表示“这条输出属于哪一轮输入”；输入生命周期帧（`input_queued` 等）
中，`input_queued` / 被拒绝的输入分别用 `accepted_uuid` / `rejected_uuid`；
`input_duplicate` 使用 `user_message_uuid`。运行事件还携带 `run_id`，启动失败未分配 ID 时省略。

## 工具调用

```json
{"type":"assistant","message":{"role":"assistant","content":[
  {"type":"tool_use","id":"call_1","name":"read","input":{"path":"/abs/go.mod","startLine":1,"endLine":3}}]}}
{"type":"user","message":{"role":"user","content":[
  {"type":"tool_result","tool_use_id":"call_1","content":"...","is_error":false}]}}
```

- `tool_use.input` 是结构化 JSON 对象，保留原始数字精度，不包含展示元数据 `_preview` 或运行时回调 `_sub_callback`。
- 审批请求的 `input` 保留字符串 `_preview`，供宿主展示 diff 等预览；审批参数哈希排除预览和回调。
  `_sub_callback` 不会进入审批或工具协议帧。
- 工具 wire ID 在**当前 turn 内唯一**：模型复用调用 ID、或不同子代理用同名 ID 时，适配层会
  为后续调用分配新 ID；`tool_result` 与审批请求都使用对应的 wire ID。
- 子代理发起的工具带扩展字段 `nekocode_subagent_id`；当前不伪造 `parent_tool_use_id`。
- 权限被拒的工具同样输出配对的 `tool_use`/`tool_result`（`is_error:true`）。
- 运行终止时，对已开始但未完成的工具补发 `is_error:true` 的结果，避免宿主留下悬空调用。

## 审批与提问

服务端通过 `control_request` 主动请求，宿主必须回应，否则该 turn 会一直等待：

```json
{"type":"control_request","request_id":"nc-1","request":{
  "subtype":"can_use_tool","tool_name":"read","tool_use_id":"call_1",
  "input":{"path":"/abs/go.mod"},"nekocode_approval":null,"nekocode_subagent_id":""}}
```
```json
{"type":"control_response","response":{
  "subtype":"success","request_id":"nc-1","response":{"behavior":"allow"}}}
```

- `behavior` 只接受 `allow` / `deny`；`allow` 只批准当前请求，不落权限规则。
- `updatedInput` 如提供必须是对象。比较时忽略 `_preview`、`_sub_callback`；完整审批 input 和
  仅含真实工具参数的回显均可，真实参数必须相同，忽略字段不写回执行数据。参数替换、非空 `updatedPermissions` 都会被拒绝，
  并把该审批按拒绝处理，同时输出 `system/error`。
- 无效/错误回复按拒绝处理；被拒绝的工具会得到 `is_error:true` 的 `tool_result`。
- 提问使用 `subtype:"nekocode_question"`，回答是二维字符串数组 `answers`（与 `questions` 顺序对应），
  拒绝用 `{"rejected":true}`。
- 运行取消或 stdin EOF 后，服务端用 `control_cancel_request` 作废这些请求；晚到/重复的
  `control_response` 会被忽略。

## 中断

```json
{"type":"control_request","request_id":"stop-1","request":{"subtype":"interrupt","cancel_queued":true}}
```

- 成功 ACK 中的 `queued_message_uuids` / `cancelled_message_uuids` 分别表示仍在队列与已被取消的输入。
- `cancel_queued:true` 会丢弃尚未启动的排队输入（不产生 `result`）；当前运行随后产出自己的
  `result`（`subtype` 为 `error_cancelled`）。
- ACK 只表示指令已受理，**必须继续读取直到 `result`** 才算该轮清理完成。

## 错误与退出

- 输入级错误：非 fatal 的 `system/error`（带 `rejected_uuid`），连接继续可用。
- 连接级故障（畸形帧、超限、输出拥堵、可靠事件订阅异常关闭等）：尽力输出
  `system/error` + `fatal:true`，随后进程以非零码退出。
- 管道已损坏时不保证还能发出错误帧或 `result`；宿主必须处理子进程退出。
- `SIGINT`/`SIGTERM`：取消当前运行、清理连接后退出。

## 限额与可靠性

- 单帧输入/输出上限约 8 MiB；连续 assistant 聚合块上限 4 MiB。
- 输入 UUID、session ID、control request ID 上限 256 字节。
- 最多 32 个排队 turn、队列正文合计最多 16 MiB；每个连接最多保留 10000 个输入 ID（超出需重连）。
- 输出队列最多 128 帧 / 合计 16 MiB；输出拥堵会**明确终止连接**，不静默丢帧。
- 服务端以 `EventFilter{Reliable:true}` 订阅运行时事件：订阅积压超限即关闭，适配层视为连接失败
  并取消运行。宿主必须把“事件流异常关闭”当作连接故障，而不是正常结束。

---

## Go 嵌入

面向仓库内或可编译进同一二进制的宿主。第三方 Go 模块若无法导入 `nekocode/*`，请使用子进程方式。

```go
// 独立使用运行时（等价于 CLI 的 headless 入口）
func RunStdio(ctx context.Context, options Options) error

// 自带 Backend 与可关闭流（in 输入、out 输出）
func Serve(ctx context.Context, in io.ReadCloser, out io.WriteCloser,
    backend Backend, cwd string, options Options) (err error)

// 只做参数解析（不消费 TUI/ACP 参数）
func ParseArgs(args []string) (Options, bool, error)

type Options struct {
    InputFormat            string // "text"（默认）或 "stream-json"
    Prompt                 string // 单次 prompt；非空则不读 stdin
    Resume                 string // 会话 ID
    IncludePartialMessages bool
}
```

`Serve` 的契约：

- **流的生命周期归 `Serve`**：返回时关闭 `in`/`out`，并取消当前运行；`Close` 必须能解除阻塞中的
  `Read`/`Write`（继承的 stdio 可能未注册到 Go poller，`RunStdio` 会自动复制为可 poll 的 fd）。
- `backend` 由调用方拥有，连接期间**独占**使用；返回前 `Serve` 会取消并等待其运行结束。
- `Backend` 需要在同一连接上同时只服务一个 turn；`Events` **必须遵守 `EventFilter.Reliable` 语义**
  （积压时关闭通道，而不是静默丢弃），否则协议无法保证不丢帧。

```go
type Backend interface {
    StartRun(context.Context, rt.Input) (rt.RunID, error)
    WaitRun(context.Context, rt.RunID) error
    CancelRun(context.Context, rt.RunID) error
    DecideApproval(context.Context, string, rt.ApprovalDecision) error
    AnswerQuestion(context.Context, string, rt.QuestionReply) error
    Events(context.Context, rt.EventFilter) (<-chan rt.Event, error) // 必须遵守 Reliable
    CurrentSessionID() string
    NewSession() (rt.SessionMeta, error)
    ResumeSession(string) error
    CurrentModel() rt.ModelSelection
    PermissionMode() string
}
```

`*runtime.Runtime` 已实现该接口（代码中有 `var _ Backend = (*rt.Runtime)(nil)` 断言）。

## 能力声明

`system/init` 与 `initialize` 应答中的 `capabilities`：

| 值 | 含义 |
|----|------|
| `text` | 文本输入输出 |
| `fifo_turns` | 输入按 FIFO 排队，一轮一跑 |
| `interrupt` | 支持 `interrupt` 控制指令 |
| `cancel_queued` | 支持取消尚未启动的排队输入 |
| `can_use_tool` | 服务端可发起工具审批请求 |
| `nekocode_question` | 支持 NekoCode 提问扩展 |
| `synthetic_content_stream` | `stream_event` 为适配层合成块，非供应商原始事件 |
| `subagent_output` | 子代理模型输出 |
| `run_summary` | 真实步数与终止原因 |
| `permission_denials` | 审批拒绝摘要 |

声明之外的能力**不支持**，宿主不要探测：宿主注入 Hooks / SDK-hosted MCP / agents / skills /
system prompt / JSON Schema / prompt suggestions（带非空值时 `initialize` 返回 control error）、
运行中切换会话、运行中切换模型、Claude 的 `local_agent`/`local_workflow` 任务生命周期。

## v2 管理能力

新增控制方法：`server.info`、`models.list`、`model.set`、`permissions.set`、
`sessions.list`、`session.history`、`session.new/resume/delete`、`extensions.list`、
`workspace.checkpoints/rewind`、`run.steer` 和 `shutdown`。
嵌入式后端的能力声明按实际服务区分：`ModelCatalog` 控制模型列表，
`Sessions` 控制会话只读查询，`SessionCreate/SessionResume/SessionDelete` 分别控制写操作。
检查点查询使用 `Checkpoints()`，与 `Rewind()` 分别声明能力，不依赖命令菜单。
标准 runtime 自动填充这些标志。

各方法是否可用以 capabilities 为准；字段与限制见
[完整方法表](../docs/STREAM_JSON.md#初始化与能力发现)。

模型、权限、会话、回滚操作要求运行和队列均空闲。历史支持 offset/limit 分页。
子代理非空完整文本使用 `subagent/message` 帧；无文本步骤不发 message，不能将它视为每步结束标记。
增量由 partial messages 开关控制。
结果增加 `stop_reason`、可选真实 `step_count` 和 `permission_denials`。
`shutdown` 支持排空或取消，不必关闭 stdin 即可退出。

## 版本与兼容

- `ProtocolVersion = "nekocode-headless/2"`；每次 `system/init` 与 `initialize` 应答都会带上它，
  宿主应据此做能力协商，并对未知帧 `type` 采取忽略（而非崩溃）策略。
- `result.nekocode_usage` 是运行时指标的**原样快照**（Turn/Context 字段），不是 Anthropic usage，
  也没有 `total_cost_usd`、`modelUsage` 等 Claude 字段，不要当费用数据使用。
- 未使用真实 Claude Agent SDK 验证过兼容性，请勿把本协议当作 Claude Code 的直接替代。

## 测试

```bash
go test -race ./headless ./runtime/... ./acp ./bot/agent/... ./bot/core ./cmd/tui
```

包内测试覆盖 NDJSON 分块、双向审批/提问、多轮排队、EOF、重复 ID、未知请求、中断工具结算、
结构化参数、超大输入与慢消费者，并含真实子进程测试（stdin 保持打开、取消、stdout 背压、
Unix 标准流阻塞模式恢复）。

## 参考

- [`docs/STREAM_JSON.md`](../docs/STREAM_JSON.md) —— 完整线协议契约（以该文档为准）
- [`docs/STREAM_JSON_GAPS.md`](../docs/STREAM_JSON_GAPS.md) —— 对标 Claude Code 的能力差距与借鉴优先级
- [`runtime/`](../runtime) —— `Backend` 背后的运行时：run、事件、审批、提问
- [`acp/`](../acp) —— 另一套 stdio 适配（ACP v1 / JSON-RPC），可作为对照实现
