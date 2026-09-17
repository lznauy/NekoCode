# 基于 NekoCode Runtime 开发上层应用

本文说明如何组装 AI 应用、接入 UI 或消息渠道。内容以
`runtime.ProtocolVersion == "2.0"` 为准。

先记住三个边界：

1. `runtime.Runtime` 是上层交互的唯一实例入口。
2. AI 应用只必须实现 `runtime.Runner`，其他能力通过 `runtime.Services` 显式装配。
3. UI 只依赖 `nekocode/runtime`，不依赖 `bot` 或 `runtime/internal`。

## 1. 先选择入口

| 目标 | 入口 | 需要编写 |
| --- | --- | --- |
| 使用完整 NekoCode | `standard.New()` | UI 或启动代码 |
| 使用完整 Bot，但不启用默认录制和 Connector | `standard.FromBot(bot)` | 启动代码 |
| 自定义提示词和工具 | `runtime.New(runner, runner.Services())` | Agent 组装 |
| 接入其他推理引擎 | `runtime.New(runner, services)` | `Runner.Run` 和 Services |
| 新增进程内 UI | 接收同一个 `*runtime.Runtime` | 事件投影 |
| 新增远程 UI | `httpapi.New(manager)` | HTTP/SSE 客户端 |
| 通过子进程接入 | `nekocode-tui --headless` | [NDJSON 客户端](../headless/README.md) |
| 新增消息渠道 | `Runtime.RegisterConnector` | `Connector` |

不需要完整 NekoCode 能力时，不要先创建 `bot.Bot` 再关闭功能。应从模型、提示词和
工具白名单开始组装独立应用。

## 2. 分层

```text
TUI / GUI / HTTP / Connector / 自定义 UI
                    |
          runtime.Runtime + Event
                    |
            Runner + 可选能力
                    |
       Agent / Provider / Tools / Policy
```

各层职责：

- **底座**：模型调用、上下文、工具、Policy 和 Agent 循环。
- **应用**：选择提示词、工具和能力，实现 `Runner`。
- **Runtime**：管理 run、事件、审批、问题、状态、录制和 Connector。
- **交互层**：把 Runtime 事件投影为终端、网页或渠道消息。

`bot.Bot` 是完整 NekoCode 产品的领域对象，不是 UI 协议。它与 Runtime 的适配只在
`runtime/standard` 中完成。

## 3. 最小应用

`Runner` 是唯一必选协议：

```go
type Runner interface {
    Run(
        ctx context.Context,
        input string,
        host runtime.RunHost,
    ) (string, error)
}
```

最小可运行程序：

```go
package main

import (
    "context"
    "fmt"
    "log"

    controlruntime "nekocode/runtime"
)

func main() {
    ctx := context.Background()
    runner := controlruntime.RunnerFunc(
        func(ctx context.Context, input string, host controlruntime.RunHost) (string, error) {
            output := "echo: " + input
            host.Text(output)
            return output, nil
        },
    )

    rt := controlruntime.New(runner, controlruntime.Services{})
    defer func() {
        if err := rt.Close(); err != nil {
            log.Printf("close runtime: %v", err)
        }
    }()

    // 必须先订阅，再启动 run，避免漏掉起始事件。
    events, err := rt.Events(ctx, controlruntime.EventFilter{})
    if err != nil {
        log.Fatal(err)
    }
    runID, err := rt.StartRun(ctx, controlruntime.Input{
        Source: controlruntime.SourceRef{Kind: "cli"},
        Text:   "hello",
    })
    if err != nil {
        log.Fatal(err)
    }

    for event := range events {
        if event.RunID != runID {
            continue
        }
        switch event.Type {
        case controlruntime.EventAssistantDelta:
            fmt.Print(event.Payload.(controlruntime.DeltaPayload).Delta)
        case controlruntime.EventRunDone:
            fmt.Println()
            return
        case controlruntime.EventRunFailed, controlruntime.EventRunCancelled:
            result, _ := event.Payload.(controlruntime.RunResult)
            log.Printf("run stopped: %s", result.Error)
            return
        }
    }
}
```

`StartRun` 是异步入口，只返回 `RunID`。最终结果从终态事件或
`LookupRun(runID)` 获取。

`Input` 的字段很少：

- `Text`：执行内容；空白输入返回 `ErrorInvalidInput`。
- `Source.Kind`：入口类型，例如 `tui`、`gui`、`http`、`feishu`。
- `Source.ID`：具体连接或会话标识，可空。
- `Sender`：展示和审计用的用户身份，可空。

## 4. 组装 NekoCode Agent

自定义 Agent 时，先建立工具白名单，再创建 Runtime：

```go
registry := tools.New(
    web.NewWebSearchTool(),
    web.NewWebFetchTool(),
)

model := provider.New(provider.Config{
    APIKey:  os.Getenv("NEKOCODE_API_KEY"),
    BaseURL: os.Getenv("NEKOCODE_BASE_URL"),
    Model:   "gpt-4o-mini",
    Protocol: "openai",
})

conversation := contextmgr.New(contextmgr.Config{
    SystemPrompt:  "You are a concise web research assistant.",
    ContextWindow: 128_000,
})

assistant := agent.New(context.Background(), agent.Config{
    Context: conversation,
    Model:   model,
    Tools:   registry,
})

runner := agentrunner.New(assistant)
rt := runtime.New(runner, runner.Services())
```

`agentrunner` 已处理流式文本、推理、工具和子 Agent 事件、取消、指标和关闭。
只有接入非 NekoCode 推理引擎时，才直接实现 `Runner`。

完整示例见
[`examples/web-assistant/main.go`](../examples/web-assistant/main.go)。该示例没有创建
Extension，也没有注册 Skill、Plugin、MCP 或编码工具，因此这些能力不会进入应用。

Policy 也是可选组合：

```go
governance := policy.New()
builtin.Register(governance)

assistant := agent.New(context.Background(), agent.Config{
    Context: conversation,
    Model:   model,
    Tools:   registry,
    Policy:  governance,
})
```

工具权限在 Registry 构造阶段限制；Policy 负责运行期审计、Hook 和确定性阻断。
不要用 Policy 模拟工具白名单。

## 5. Runner 如何输出

一次 `Run` 独占一个 `RunHost`：

```go
type RunHost interface {
    Text(delta string)
    Reason(delta string)
    Step(protocol.StepEvent)
    Phase(string)
    Todos([]TodoItem)
    Confirm(ConfirmRequest) ConfirmReply
    Ask(QuestionRequest) QuestionReply
}
```

常用规则：

- `Text` 和 `Reason` 接收增量，不是完整历史。
- `StepEvent` 使用稳定的 `CallID` 配对工具的 start、preview、blocked、execute 动作。
- 工具动作通过 `ToolInput` 提供结构化 JSON 参数；`ToolArgs` 是展示文本，不应作为机器输入解析。
- `StepActionChat` 上报完整文本，投影为 `assistant_message`；`Text` 仍用于增量预览。
- `StepActionRunSummary` 通过 `Summary` 上报真实步数和终止原因，投影为 `run_summary`。
- 子 Agent 动作使用 `SubAgentID` 关联，不把身份编码进字符串；`StepActionSubAgentText`、`StepActionSubAgentReason`、`StepActionSubAgentMessage` 分别上报文本增量、思考增量和非空完整文本，投影为 `subagent_output`。
- `Confirm`、`Ask` 会阻塞 Runner，直到 UI 回复、run 取消或 Runtime 关闭。
- `Run` 返回后不得保存或继续调用 `RunHost`。

交互请求不需要 Runner 管理 ID 或等待队列：

```go
reply := host.Confirm(runtime.ConfirmRequest{
    ToolName: "shell",
    Args:     map[string]any{"command": "go test ./..."},
    Kind:     runtime.ConfirmKindPermission,
})
if !reply.Allowed {
    return "", errors.New("operation rejected")
}
```

Runtime 会串行处理同一个 run 的 `Confirm` 和 `Ask`，UI 同时只需展示一个请求。

## 6. 可选能力

`runtime.New(runner, runtime.Services{})` 只启用核心执行。应用需要命令、Steering、指标、模型、上下文、
Extension、配置或 Session 时，在组合根通过一个显式 `runtime.Services` 值装配：

```go
services := runtime.Services{
    Steer:        assistant.Steer,
    Metrics:      assistant.Metrics,
    CurrentModel: assistant.CurrentModel,
    Close:        assistant.Close,
}
rt := runtime.New(assistant, services)
```

Runtime 根据非 nil 函数字段生成 `Capabilities()`。能力不会通过小接口和类型断言
自动发现，因此组合关系在初始化代码中是完整、可检查的。UI 仍只通过 Runtime 查询：

```go
caps := rt.Capabilities()
if caps.Models {
    model := rt.CurrentModel()
    _ = model
}
if caps.Context {
    snapshot := rt.ContextSnapshot()
    memory := rt.MemoryView(runtime.MemoryScopeProject)
    _, _ = snapshot, memory
}
```

Runtime 的可选只读方法：

| 能力 | 查询方法 |
| --- | --- |
| Models | `CurrentModel` |
| ModelCatalog | `ModelOptions` |
| ToolCatalog | `ToolNames` |
| Checkpoints | `Checkpoints`（返回结构化恢复点及查询错误） |
| Context | `ContextSnapshot`、`MemoryView` |
| Extension | `SkillManagementView` |
| Configuration | `ConfigView` |
| Session | `CurrentSessionID`、`ListSessions`、`SessionMessages` |
| Metrics | `Metrics` |
| Commands | `CommandMenu`（`/` 查询根命令，完整命令查询下一级候选） |

先用 `Capabilities()` 决定页面和控件是否存在。能力不存在或 Runtime 已关闭时，
多数只读方法返回对应零值；`Checkpoints()` 返回 `unsupported` 或 `closed` 协议错误。

`Models` 仅表示当前模型查询，模型列表和切换分别检查 `ModelCatalog`、`ModelSelection`。
`Sessions` 表示会话查询，创建、恢复、删除分别检查 `SessionCreate`、`SessionResume`、
`SessionDelete`。权限修改检查 `PermissionControl`，检查点查询和回滚分别检查
`Checkpoints`、`Rewind`。不要从只读能力推断写操作可用。

写能力同样通过 `Services` 中的函数显式提供：

```go
services := runtime.Services{
    SwitchModel:      assistant.SwitchModel,
    ApplyConfig:      assistant.ApplyConfig,
    ResumeSession:    assistant.ResumeSession,
    NewSession:       assistant.NewSession,
    DeleteSession:    assistant.DeleteSession,
}
```

UI 仍调用 `Runtime.SwitchModel`、`ApplyConfig`、`NewSession` 等方法。Runtime 会统一
检查 `closed`、`busy` 和能力是否存在；不要缓存 Runner 后绕过 Runtime 修改状态。

命令能力的结果只有三种：

- `CommandIgnored`：不是命令，继续执行 Runner。
- `CommandHandled`：命令已完成，本次 run 结束。
- `CommandContinue`：命令修改状态后，用 `AgentInput` 继续执行 Runner。

Runner 本身只需要保留核心协议的编译期断言：

```go
var _ runtime.Runner = (*assistant)(nil)
```

## 7. Runtime 对外能力

上层只保留一个 `*runtime.Runtime`，按场景使用以下方法：

| 场景 | 方法 |
| --- | --- |
| 核心交互 | `StartRun`、`CancelRun`、`Events` |
| 审批和问题 | `DecideApproval`、`AnswerQuestion` |
| 可选中途输入 | `SteerRun` |
| 生命周期监控 | `Status`、`Capabilities` |
| Run 快照 | `CurrentRun`、`LookupRun`、`Runs` |
| 可选业务视图 | 第 6 节的直接查询方法 |
| Connector | `RegisterConnector`、`Connect`、`Disconnect`、`ConnectView` |
| 事件恢复 | `ReplayEvents`、`EnableDefaultEventRecording` |
| 关闭 | `Close` |

`runtime.Interaction` 只包含所有交互端都需要的五项能力：

```go
type Interaction interface {
    StartRun(context.Context, Input) (RunID, error)
    CancelRun(context.Context, RunID) error
    DecideApproval(context.Context, string, ApprovalDecision) error
    AnswerQuestion(context.Context, string, QuestionReply) error
    Events(context.Context, EventFilter) (<-chan Event, error)
}
```

它刻意不包含管理页面、读模型、Connector 和关闭方法。装配层持有 Runtime 并负责
关闭；UI 根据自己的页面定义窄接口，不需要再包装一层 facade。

## 8. 接入一套新 UI

通用 UI 的最小消费接口可以是：

```go
type Runtime interface {
    runtime.Interaction
    Status() runtime.RuntimeStatus
    Capabilities() runtime.CapabilityManifest
    CurrentRun() (runtime.RunSnapshot, bool)
}
```

有 Session 或模型页面时，再把实际使用的方法加入该 UI 的接口。测试中使用 fake，
生产环境传入同一个 `*runtime.Runtime`。

UI 的基本流程：

1. 启动时订阅 `Events`。
2. 用 `StartRun` 提交输入，并记录返回的 `RunID`。
3. 只把目标 `RunID` 的事件投影到当前对话。
4. 渲染增量、工具、状态和终态。
5. 回复 approval 和 question。
6. 提供 `CancelRun`。
7. 断线恢复时先读 `RunSnapshot`，需要历史加实时流时再用 `ReplayEvents`。

通用 UI 必须处理三个终态：

- `EventRunDone`
- `EventRunFailed`
- `EventRunCancelled`

`RunResult.Output` 是最终快照。如果 UI 已渲染 `assistant_delta`，终态只结束
streaming 并校准内容，不要再次追加完整输出，否则会显示两遍。

## 9. 事件和状态

事件公共字段：

```go
type Event struct {
    Version  string
    ID       string
    Sequence uint64
    RunID    RunID
    Type     EventType
    Source   SourceRef
    Time     time.Time
    Payload  any
}
```

事件分组如下：

| 分组 | EventType | Payload |
| --- | --- | --- |
| 输入与输出 | `input_accepted`、`system_message`、`assistant_delta`、`assistant_message`、`reasoning_delta` | `MessagePayload` / `DeltaPayload` |
| 执行过程 | `phase_changed`、`todos_updated` | `PhasePayload` / `[]TodoItem` |
| 工具与子 Agent | `tool_started`、`tool_blocked`、`tool_preview`、`tool_completed`、`subagent_started`、`subagent_ended`、`subagent_output` | `ToolPayload` / `SubAgentPayload` / `SubAgentOutput` |
| 人机交互 | `approval_requested`、`approval_resolved`、`question_requested`、`question_resolved` | `ApprovalView` / `QuestionView` |
| 生命周期 | `run_started`、`run_summary`、`run_done`、`run_failed`、`run_aborted` | 无 / `RunSummary` / `RunResult` |
| 其他状态 | `session_changed`、`connector_status`、`metrics_updated` | `SessionPayload` / `ConnectorStatusPayload` / `MetricsSnapshot` |

Go 符号 `EventRunCancelled` 的 wire value 是 `run_aborted`；
`RunCancelled` 的 wire value 是 `aborted`。

订阅方式：

```go
live, err := rt.Events(ctx, runtime.EventFilter{RunID: runID})

events, err := rt.ReplayEvents(ctx, runtime.EventFilter{
    RunID: runID,
    After: lastSequence,
})
```

- `Events`：只接收订阅后的实时事件。
- `ReplayEvents`：先回放保留事件，再继续接收实时事件。
- `EventFilter`：支持 `RunID`、`After`、`Types`、`Sources` 和 `Reliable`。
- 默认订阅在积压时可能丢弃部分事件。传输层可设置 `Reliable: true`：积压超限直接关闭订阅，调用方必须把异常关闭视为流失败，并取消对应运行或重建连接；这不提供持久化或自动重传保证。

事件是实时投影，不是可靠消息队列。重连时以 `RunSnapshot` 为恢复基线，以
`Sequence` 为增量游标。消费循环应快速投递到 UI 队列，避免被长操作阻塞。

Runtime 状态只有三种：

| `RuntimeState` | 含义 |
| --- | --- |
| `ready` | 可以接受 run |
| `busy` | 正在运行、等待交互或修改可选能力 |
| `closed` | 已关闭 |

单个 Runtime 同时只允许一个 active run。具体 run 状态为 `running`、
`waiting_approval`、`waiting_question`、`done`、`failed`、`aborted`。
生命周期读 `Status()`，业务指标读 `Metrics()`，二者不要混用。

## 10. Approval、Question 和错误

UI 从请求事件中取得 Runtime 生成的 ID：

```go
case runtime.EventApprovalRequested:
    view := event.Payload.(runtime.ApprovalView)
    err := rt.DecideApproval(ctx, view.ID, runtime.ApprovalDecision{
        Allowed: true,
    })

case runtime.EventQuestionRequested:
    view := event.Payload.(runtime.QuestionView)
    err := rt.AnswerQuestion(ctx, view.ID, runtime.QuestionReply{
        Answers: [][]string{{"concise"}},
    })
```

关闭问题弹窗时传 `QuestionReply{Rejected: true}`。原始工具参数位于
`ApprovalView.Args`；动态 Shell 结构、能力、授权范围、工作区和可写目录
位于强类型 `ApprovalView.Approval`。`Allowed: true` 原子覆盖该事件中
已展示的全部内容。
`CanEscalatePermission` 和 `AllowWithPermission` 仅为旧 Go 客户端保留，
新代码不应再设置。

控制方法使用稳定的 `ProtocolError.Code`：

| Code | 含义 |
| --- | --- |
| `invalid_input` | 输入无效 |
| `closed` | Runtime 已关闭 |
| `busy` | 当前有 run 或状态修改 |
| `not_found` | 没有 active run |
| `conflict` | RunID 或当前状态不匹配 |
| `unsupported` | Runner 未实现该能力 |

```go
var protocolErr *runtime.ProtocolError
if errors.As(err, &protocolErr) && protocolErr.Code == runtime.ErrorBusy {
    // 聚焦现有 active run。
}
```

不要匹配错误字符串。

## 11. 标准应用、HTTP 和 Connector

完整 NekoCode：

```go
rt, err := standard.New()
if err != nil {
    return err
}
defer func() {
    if err := rt.Close(); err != nil {
        log.Printf("close runtime: %v", err)
    }
}()
```

`standard.New()` 会组装完整 Bot、默认事件录制，以及 Telegram、Feishu、QQBot
Connector。轻量应用使用 `runtime.New(customRunner, services)`。

HTTP/SSE：

```go
handler := httpapi.New(rt).Handler()
handler = httpapi.WithBearerAuth(handler, token)
server := &http.Server{Addr: "127.0.0.1:8765", Handler: handler}
```

非本机监听必须启用认证。Endpoint 和 cursor 规则见
[`RUNTIME_HTTP_API.md`](RUNTIME_HTTP_API.md)。

Connector 只依赖窄化的 `ConnectorRuntime`。实现：

```go
type Connector interface {
    Name() string
    Start(context.Context) error
    Stop() error
    HandleCommand(context.Context, []string) (string, error)
}
```

注册：

```go
rt.RegisterConnector("my-channel", func(host runtime.ConnectorRuntime) runtime.Connector {
    return mychannel.New(host)
})
```

Connector 负责外部消息和 Runtime `Input`/`Event` 的双向映射；共享的生命周期、
配对和事件转发组件位于 `interaction/connect`。

## 12. 录制、装饰和生命周期

事件录制必须显式处理错误：

```go
if err := rt.EnableDefaultEventRecording(); err != nil {
    return errors.Join(err, rt.Close())
}
```

自定义目录使用 `EnableEventRecording(path)`。空路径是错误，不要忽略。启用时会先
恢复已有事件、run snapshot 和 sequence，再追加新事件。

装饰 Runner 时只需转发核心 `Run`：

```go
type limitedAssistant struct {
    *assistant
    maxInput int
}

func (a *limitedAssistant) Run(
    ctx context.Context,
    input string,
    host runtime.RunHost,
) (string, error) {
    if len(input) > a.maxInput {
        return "", fmt.Errorf("input exceeds %d bytes", a.maxInput)
    }
    return a.assistant.Run(ctx, input, host)
}
```

可选能力保存在组合根的 `Services` 中，不受 wrapper 方法集影响；需要改变能力时，
显式替换对应函数字段。

生命周期约束：

- 一个应用共享一个 Runtime，不为每套 UI 或 Connector 重建实例。
- `StartRun` 的 context 控制 run；订阅 context 只控制订阅。
- HTTP 请求结束后仍需继续运行时，显式使用 `context.WithoutCancel`。
- Runner 应响应 context 取消并尽快停止。
- active run 期间，模型、配置、Extension 和 Session 修改会返回 `ErrorBusy`。
- `RunHost` 在 Runner 返回、取消或关闭后失效。
- 应用所有者调用一次 `Close()` 并处理错误；Runner 或事件回调不得同步调用它。

## 13. 验收清单

新 AI 应用：

- [ ] 只注册允许的工具。
- [ ] NekoCode Agent 使用 `agentrunner.New`。
- [ ] 按需启用 Policy 和可选能力。
- [ ] 用 `Capabilities()` 断言实际暴露面。
- [ ] 处理录制和关闭错误。

新 UI：

- [ ] 定义自己的窄 Runtime 接口。
- [ ] 先订阅，再启动 run。
- [ ] 按 `RunID` 分发，覆盖三个终态。
- [ ] 支持 approval、question 和取消。
- [ ] 根据 `Capabilities()` 组合可选页面。
- [ ] 重连时使用 snapshot 和 sequence 恢复。

关键测试：

- Runner 响应 context 取消，正确返回 output/error。
- 工具事件有稳定 `CallID`。
- approval/question 拒绝路径不会阻塞。
- UI 不重复追加 `RunResult.Output`。
- 受限应用的 Registry 和 CapabilityManifest 不会意外扩大。

## 14. 参考实现

- 受限应用：[`examples/web-assistant/main.go`](../examples/web-assistant/main.go)
- 标准组装：[`runtime/standard/standard.go`](../runtime/standard/standard.go)
- 核心协议：[`runtime/runner.go`](../runtime/runner.go)
- 可选能力：[`runtime/runtime_services.go`](../runtime/runtime_services.go)
- 公开事件和 DTO：[`runtime/protocol.go`](../runtime/protocol.go)
- Agent 适配：[`runtime/agentrunner/agentrunner.go`](../runtime/agentrunner/agentrunner.go)
- TUI：[`interaction/tui/model.go`](../interaction/tui/model.go)
- GUI：[`interaction/gui/app/app.go`](../interaction/gui/app/app.go)
- HTTP：[`runtime/httpapi/httpapi.go`](../runtime/httpapi/httpapi.go)
- Connector：[`interaction/connect/connect.go`](../interaction/connect/connect.go)
