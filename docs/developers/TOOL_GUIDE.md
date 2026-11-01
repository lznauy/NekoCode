# 自定义工具开发指南

> **主要读者：** 使用方二次开发者。编写 Go 工具并编译进自己的宿主应用；项目工具开发者也可参考。

本文实现一个 `uppercase_text` 工具，把短文本转换为大写，并说明注册、取消、权限和测试的接入方式。示例不访问文件、网络或模型，可直接验证。

工具是 Go 代码，实现后需要编译进宿主应用。插件 JSON、Skill 和 Agent 配置不会动态加载 Go 实现；需要从插件调用外部能力时，可以考虑 MCP，或 [Hook 命令动作](HOOK_GUIDE.md)。

## 一、工具接口

所有工具实现 [core.Tool](../../bot/extension/tool/runtime/core/types.go)：

| 方法 | 作用 |
| --- | --- |
| `Name()` | 模型调用名称，建议简短且唯一 |
| `Description()` | 说明用途和使用条件 |
| `Parameters()` | 声明模型侧参数结构 |
| `ExecutionMode(args)` | 选择并行或顺序执行 |
| `Execute(ctx, args)` | 校验输入，执行操作并返回 `ToolOutput` 或错误 |

参数描述用于生成请求结构，不能代替执行时的校验。模型、测试或其他调用方都可能传入缺字段、类型错误或不符合业务约束的参数。

## 二、运行完整示例

在仓库根目录创建临时目录 `.tool-demo/`，把下面程序保存为 `.tool-demo/main.go`。不要在根目录再添加一个 `main`，以免影响常规构建。

```go
package main

import (
    "context"
    "fmt"
    "strings"
    "unicode/utf8"

    tools "nekocode/bot/extension/tool"
    "nekocode/bot/extension/tool/runtime/core"
    "nekocode/bot/extension/tool/runtime/toolutil"
)

type UppercaseTool struct {
    toolutil.SafeReadOnlyTool
}

var _ core.Tool = (*UppercaseTool)(nil)

func (*UppercaseTool) Name() string { return "uppercase_text" }

func (*UppercaseTool) Description() string {
    return "Convert at most 128 characters of text to uppercase."
}

func (*UppercaseTool) Parameters() []core.Parameter {
    return []core.Parameter{{
        Name: "text", Type: "string", Required: true,
        Description: "Non-empty text, at most 128 Unicode characters",
    }}
}

func (*UppercaseTool) Execute(ctx context.Context, args map[string]any) (core.ToolOutput, error) {
    if err := ctx.Err(); err != nil {
        return core.ToolOutput{}, err
    }
    text, err := toolutil.RequireStringArg(args, "text")
    if err != nil {
        return core.ToolOutput{}, err
    }
    if utf8.RuneCountInString(text) > 128 {
        return core.ToolOutput{}, fmt.Errorf("text exceeds 128 characters")
    }
    return core.ToolOutput{Text: strings.ToUpper(text)}, nil
}

func main() {
    registry := tools.New()
    registry.RegisterWithOptions(&UppercaseTool{}, tools.RegistrationOptions{
        PlanAllowed: true,
    })
    entry, err := registry.Lookup("uppercase_text")
    if err != nil {
        panic(err)
    }
    output, err := entry.Tool.Execute(context.Background(), map[string]any{
        "text": "Hello NekoCode",
    })
    if err != nil {
        panic(err)
    }
    fmt.Println(output.Render())
}
```

在仓库根目录运行：

```bash
go run ./.tool-demo
```

预期输出 `HELLO NEKOCODE`。这是直接调用测试，不会启动 Agent，也不经过执行器的审批、Hook 或沙箱流程。

`SafeReadOnlyTool` 在这里仅提供并行执行方法，不会检查代码是否只读，也不自动授予权限。示例是无副作用的纯转换，所以显式设置 `PlanAllowed: true`；其他工具应按实际行为决定是否允许在 Plan Mode 使用。

## 三、交给 Agent 使用

自定义组装应用时，把上面的 `registry` 传入 `agent.Config.Tools`。创建模型、上下文和 Runtime 的完整步骤见 [Runtime 应用开发指南](RUNTIME_APP_GUIDE.md#4-组装-nekocode-agent)。

若使用标准工具箱，在构造时加入额外工具：

```go
toolbox := catalog.NewToolboxWithConfig(catalog.ToolboxConfig{
    ExtraTools: []core.Tool{&UppercaseTool{}},
})
defer toolbox.Close()
```

这段需要导入 `nekocode/bot/extension/tool/builtin/catalog`，并放在能访问 `UppercaseTool` 的宿主代码中。构造工具箱后使用它的 `Registry`。`ExtraTools` 会随工具箱重建保留；只向一次性的旧 Registry 注册，配置切换重建后可能丢失。

工具箱的额外工具注册不等于示例中的 `PlanAllowed` 元数据设置。需要该选项时，在每次构建对应 Registry 时明确注册；不要假设只读工具名会自动放行。

注册同名工具会替换旧实现，并清除旧的注册元数据。避免使用内置名称；检查宿主实际提供的名称，不能只看某份 Agent 的白名单。自定义子 Agent 要使用新工具，还需要宿主已经注册该工具，并在 Profile 的 `tools` 中列出它；声明 base 时仍受基线上限约束。

## 四、带副作用的工具需要额外处理

| 行为 | 实现要求 |
| --- | --- |
| 读取文件 | 使用工作区感知的路径校验，参考 `ValidatePathReadableContext` 和内置 read/list |
| 修改文件 | 使用写路径校验，并接入相应预览、审批、写前读和检查点链路；不要只调用 `os.WriteFile` |
| 执行外部程序 | 使用可取消的进程、受控工作目录和明确的执行环境；复用现有 Shell 执行边界 |
| 网络调用 | 传递 context、设置请求期限、限制响应体和输出大小 |
| 代理其他工具 | 通过 `ResolveTarget` 声明真实目标，让审批、Hook 和审计看到正确身份 |

`ModeParallel` 适合互不影响的调用；共享状态、写文件或依赖调用顺序时使用 `ModeSequential`，必要时自行加锁。顺序模式不代替对并发宿主访问的保护。

`RegistrationOptions` 还提供 `Preview`、`PermissionPlan`、`Privileged` 等接入点，定义见 [工具 Registry](../../bot/extension/tool/tools.go)。它们分别用于展示预览、预测权限请求及在明确批准后重试，不是给任意自定义代码自动套上沙箱。

工具参数键叫 `path`，不会自动获得所有内置文件工具的保护。需要写文件时，先阅读 [内置 edit 实现](../../bot/extension/tool/builtin/filesystem/edit/tool_edit.go) 及执行器流程，再决定复用哪些能力。

内置文本 read/write/edit 通过 `execution.LockFile` 共用规范目标锁；修改已有文件时，还会检查当前执行者实际收到的读取预览所对应的原始版本。`ReadID` 仅用于当前模型请求的内存交付，不可从会话恢复或复制给子执行者。直接调用工具的嵌入方需提供独立 `ExecutionState` 才启用版本门禁；具体范围见 [文件版本与修改前提](../maintainers/ARCHITECTURE_BOUNDARIES.md#文件版本与修改前提)。

## 五、取消、错误与输出

耗时操作必须主动响应 `ctx`；取消后停止子请求和后续副作用，不要留下后台 goroutine。内存计算也应限制输入规模，长循环中检查 context。

失败返回 `error`，不要把失败伪装成成功的 `ToolOutput.Text`。可交付的部分结果是否保留，应根据调用链约定处理，不能只打印日志后返回空成功。

短输出用 `core.ToolOutput{Text: ...}`。大结果应接入执行器的有界输出与文件引用机制，参考 [输出类型](../../bot/extension/tool/runtime/core/output_budget.go)，不要把无限量数据塞进模型上下文。授权依据来自结构化工具结果与执行上下文，不来自输出中一句“已读取”或“已授权”。

工具有真实的进程状态时，可通过 `ToolOutput.Process` 返回 `core.ProcessObservation`；executor 在正常返回、错误与权限重试结果中保留它。`ExitCodeKnown` 只能来自真实正常退出；`OutputComplete` 只能表示当前样本包含全部已捕获输出，未知时保持 false。没有进程事实的工具保持零值，不能根据日志里的“成功”文字或 nil error 填充。它不自动改变工具 error 或模型展示。executor 在实际调用边界生成宿主凭证，普通调用与权限重试分别记录；凭证只随工具 transcript 保存，工具本身不能指定或覆盖 ID。不要将 nil error、展示摘要或凭证存在解释为业务成功，具体见[工具执行凭证](../maintainers/ARCHITECTURE_BOUNDARIES.md#工具执行凭证)。

## 六、验证建议

至少覆盖正常输入、缺失或错误类型、超限输入、已取消 context。访问文件或网络的工具还需覆盖越界路径、权限拒绝、超时以及副作用失败。

示例可以在临时目录内添加 `main_test.go`，用标准 Go 测试调用 `Execute`，然后运行 `go test ./.tool-demo`。直接调用只验证实现；接入 Agent 后，还要确认模型能看到参数定义，执行器实际应用了预期的权限和审计。

完成后移走 `.tool-demo/`。需要通过事件控制工具调用时，继续阅读 [Hook 指南](HOOK_GUIDE.md)；需要让子 Agent 使用它时，阅读 [Agent 指南](AGENT_GUIDE.md)。

## 技能与已保存输出的引用查询

内置 `context_refs` 复用当前执行者历史中的 `ToolOutputArtifact`，不会把自定义工具的普通路径文本猜成产物。自定义工具需要沿现有 `ToolOutput.Artifact` 返回结构化引用，并由当前执行者的 OutputStore 持有文件。

模型使用 `action=list` 查来源页，`before=0` 从最新记录开始，`limit` 为 1–20 条含引用的来源记录，历史索引跳过普通消息；has_more 表示还有更早引用。`action=inspect` 接受 list 返回的 ID，产物通过检查后返回当前可读取的位置，正文用 read 重取；技能则核对已接纳版本与当前注册内容是否相同，正文仍需通过允许的 skill/read 入口重取。它不提供任意路径查找、不新增授权，也不能用 available 推断业务成功或文件内容未被修改。主执行者查询自身会话；子执行者查询自身任务，不能指定父任务、其他子任务或会话。内置 coder/explore 已开放，自定义 Profile 需将 context_refs 加入 tools；缺完整身份的嵌入执行不开放查询。ready 技能只表示宿主完整文本就绪，不是 provider 接收回执。完整边界见 [技能接纳与引用](../maintainers/ARCHITECTURE_BOUNDARIES.md#技能接纳与引用) 和 [产物引用与查询](../maintainers/ARCHITECTURE_BOUNDARIES.md#产物引用与查询)。

## 执行凭证查询

只读 `evidence(action="list", before=0, limit=20)` 查询当前执行者历史，凭证条目位于 evidence 数组（无条目时该字段可省略）。limit 限原始记录扫描数，同时最多返回 20 条凭证；无凭证条目的页仍需检查 has_more 并用 before 继续。`evidence(action="inspect", id="evidence.…")` 定位 list 给出的引用，detail 返回该次物理调用及交付摘要，status=recorded 只表示来源匹配。receipt_id 是原始执行 UUID；查询引用另绑定当前 scope 和原始序号。

查询不返回产物路径或内容，不读取文件，也不授予权限。主代理可配对提供 `task_id` 与 `attempt_id`，只查当前父会话下该子执行的已提交区间；子代理只能查自己的任务（含历史 attempt），不能使用范围选择器。内置 coder/explore 已开放，自定义 Profile 需显式允许 evidence；该工具与 context_refs 分别授权，不自动投影完整凭证到模型请求。
