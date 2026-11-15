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

查询不返回产物路径或内容，不读取文件，也不授予权限。主代理可配对提供 `task_id` 与 `attempt_id`，只查当前父会话下该子执行的已提交区间；子代理只能查自己的任务（含历史 attempt），不能使用范围选择器。内置 coder/explore 已开放，自定义 Profile 需显式允许 evidence；该工具与 context_refs 分别授权。主模型、合成请求和允许 evidence 的子模型自动接收最多 16 条近期凭证引用；与技能/产物合计仍限 4 KiB，仅投影元数据，不携带完整凭证。自动采样限最近 20 条原始记录，旧凭证需 list 分页或按已知 ID inspect。

### 托管进程等待与重复启动

shell 的短观察窗口与 process 的 wait_timeout 都不表示命令失败。等待返回仍在运行时，反馈包含 task、new_output_bytes（相对上次观察的原始捕获字节增量）、unchanged_waits；不会自动附带再次等待的 next_call。观察超时会提示检查服务就绪或其他进展证据，连续两次没有新输出会附加诊断提示，不自动停止进程；新输出或终态会清除空等计数。日志安静不代表没有 CPU、磁盘或业务进展，计数不作卡死判定。

匿名 shell 启动在同一注册表内对完全相同的活跃请求复用原进程，明确返回 reused_running/no new command。匹配包含执行者/会话、精确命令、目录、host 标志、完整授权沙箱配置、硬生命周期以及继承环境；仅保存摘要，不输出环境值。并发启动的检查和注册串行完成，观察等待不持启动锁。不同权限、会话、环境或生命周期不合并；不同显式 name 允许有意并行，相同显式 name 仍拒绝重复。进程结束后可重新启动；返回已复用的进程不会重置其超时或等待历史。

重试前先确认原进程终态并检查部分副作用。该机制不解析命令语义，也不识别改写命令或自行套用 shell timeout 后的语义等价请求；反馈和工作流规则要求先诊断，不能把这些改写当成自动重试授权。

process.wait 和 watch 每次有有限观察窗口，默认 30 秒，可传 wait_timeout_ms（正数，最多 120000 毫秒）调整。wait/exit 等退出，watch/output 等新输出或退出；在事件、窗口到期或取消三者中取先发生者。观察到期返回 wait_timeout 和仍在运行的状态，不杀进程，也不把静默判为卡死。工具返回一次观察结果，由模型决定下一步；工具内部不循环唤醒模型或重复等待。shell.timeout_ms 仍是会终止命令的硬生命周期，两者不可混淆。启动托管任务不再统一建议 wait；常驻服务应验证就绪，而不是等待退出。

上下文达到现有压缩压力时，Context 可先把已有完整产物的旧检索结果收为短预览，近期配对和错误保留。完整记录和产物仍在；短预览不授权修改源文件，也不能据此宣称已经重新核实了被省略内容。无需为检查点额外调用摘要模型，无压力时保持已有缓存前缀。


### 隔离网络中的本地服务验证

Linux 默认每条 shell 命令有自己的网络命名空间。同一命令内的服务和浏览器/HTTP客户端可以使用 loopback；另一条 shell 命令的 localhost 不指向已有服务。共享 $NEKOCODE_SCRATCH 不代表共享网络。该变量是 shell 命名空间内的路径，文件应由 shell 创建和读取；read/write/edit 等文件工具不会将同名宿主路径映射到 actor 缓存。需要跨工具使用的脚本或截图放在授权工作区。把启动、有限就绪探针、客户端验证与退出清理放在同一条命令。Chrome 使用 scratch 下可写的独立 user-data-dir。

示例（通过一条 shell 调用执行）：

```bash
set -eu
python3 -u -m http.server 8000 --bind 127.0.0.1 > "$NEKOCODE_SCRATCH/http.log" 2>&1 &
server_pid=$!
trap 'kill "$server_pid" 2>/dev/null || true; wait "$server_pid" 2>/dev/null || true' EXIT
python3 - <<'PY'
import time, urllib.request
for attempt in range(50):
    try:
        urllib.request.urlopen('http://127.0.0.1:8000/', timeout=0.2).close()
        break
    except OSError:
        time.sleep(0.1)
else:
    raise SystemExit('local server did not become ready')
PY
profile=$(mktemp -d "$NEKOCODE_SCRATCH/chrome-XXXXXX")
google-chrome --headless=new --no-sandbox --disable-gpu \
  --user-data-dir="$profile" --screenshot="$NEKOCODE_SCRATCH/page.png" \
  http://127.0.0.1:8000/
```

这里的 --no-sandbox 仅是让 Chrome 在既有命令沙箱内运行，不是 shell 的 sandbox_mode=host。测试结束后将需要保留的证据导出到授权工作区。

成功的 `write` / `edit` 回执与 `read` 共用进程内的文件版本凭证：只有完整回执确实进入当前执行器的模型上下文，且磁盘原始字节未改变，才能继续修改该版本。回执被裁剪、移除、会话恢复或切换 agent 都不能沿用凭证。较大的 `write` diff 在受管执行中保存为完整结果产物，模型收到简短确认与产物引用；保存产物失败则保留原始内联 diff。交互确认预览仍保留完整 diff。


### 图片检查与凭据输出

`read` 对图片仅返回格式和尺寸，不传递像素，也不构成视觉检查。使用 `image_understand` 检查画面；其 `model` 参数的 enum 提供安全的已配置模型名称，省略时按 mode 选择。普通截图优先 fast，按具体疑点批量检查相关视图；不要为发现模型名称读取应用配置。HTTP 失败仅返回状态与恢复指引，不回显供应商响应正文。

应用加载/保存配置时，在进程内登记明确命名的凭据值。工具文本、完整结果产物、搜索输出捕获、shell 输出流和诊断日志在交付/落盘前脱敏已登记值，处理跨 chunk 的完整凭据及 JSON 转义拼写，凭据轮换后已有流也会更新。技能引用以脱敏后的实际交付内容核验连续性。

这是避免意外明文回显的保护，不是任意秘密探测、操作系统文件隔离或防恶意编码外传；不会清理历史原始记录。配置仍可能被程序读取，尚未登记的值、派生/编码值以及主动截取的片段不在保证内。

`image_understand` 的模型尝试写入同一 `llm-calls.jsonl`，带父会话、执行身份和 `tool_call_id`；工作区权限身份不作为日志归属。JSON、SSE 与 Anthropic usage 会归一化；SSE 在内容完成后继续收集 usage，直到 `[DONE]` 或正常 EOF。缺失 usage 会明确标记 `usage_reported=false`；传输超时由 HTTP 请求期限统一管理并返回错误。


### 后台任务的阶段交接

Super 模式 `task_submit` 使用 `max_steps` 限制本次 attempt，默认 12 个普通模型轮次，并受已保存的 profile 上限约束。默认值在装配层加载实际 profile 后统一计算；显式超限的 `max_steps` 会报错，不静默截断。模型显式请求上限为 150，host profile 的配置上限为 200；同步子代理省略 `max_steps` 时继承 profile，profile 未配置时使用默认 150。它在完整工具批次后停止普通工具，只开放最终交接；未完成项返回 partial，不能满足 `after` 的成功依赖。协调者依据已保存的 evidence、验证结果和 unfinished 决定下一步，使用同一个 `resume_id` 保留历史和工作区，并设置下一阶段额度、具体下一步及停止条件。不要为读取旧报告另建任务。阶段交接复用已有持久化 attempt 和完成通知，不是后台任务仍在运行的实时 checkpoint，也不会自动续跑。

执行器通过步骤和全树调用预算限制成本；预算耗尽后按原协议交接，由协调者结合证据和 unfinished 决定是否继续。工具批次是否重复不参与进展或完成判定。

收尾请求不再为了自动压缩阈值额外调用模型；仍校验真实上下文上限。确实放不下时保留完整历史并交回 partial，避免静默删证据后声称完成。

### 通用交接与证据读取

核心与任务工作流的边界见 [通用 Harness 设计](AGENT_HARNESS.md)。

后台执行器一次返回完整结构化交接或纯文本结果。Runtime 按 task/attempt 保存不可变记录，再生成有界通知；结构化预览超限时使用通知中的 `read_result`（含固定 `result_ref`）读取同一记录，不依赖另一份格式化文本。`review_complete=true` 表示结构化报告已完整送达；纯文本执行器使用 `result_complete`。新引用与精确 attempt 查询不依赖调度历史缓存，旧 child-session 引用仍可读取；当前 attempt 的提交失败不会回退成旧文本成功。

`task_result.next_call` 使用短 `cursor`，续页只传 cursor。主机保存精确的任务、attempt、内容引用、offset 和 limit，每次仍调用当前 reader 做权限及内容绑定校验。句柄在同进程 reader 重绑后仍可用，最多保留 128 个；重启或淘汰后，用保存的 id 和确切 attempt_id 从 offset 0 恢复。错误不会自动切到最新 attempt；`recovery_call` 提供明确恢复入口。长期证据共享仍使用 result_ref。

显式指定非零 offset 时必须同时提供 result_ref；id 和 attempt_id 不再通过缓存隐式补全引用。最后一页不占用 cursor 缓存。

通用 executor 不提供 findings schema、task_findings 工具或审查专用的收尾门禁。任务特有的报告结构由委托提示词、技能或外部验收器定义；生产协议不再兼容旧实验的 findings 字段，也不要求其他任务补齐。

`nekocode_submit_result` 新增可选 outcome 与 next_action：

| outcome | 含义 | 独立 batch 中的交付 |
|---|---|---|
| complete | 委托范围完成，不得有 unfinished | 等批次结束 |
| continue | 当前无需决策，原任务仍有下一步 | 等批次结束 |
| blocked | 缺少执行前置条件 | 及时唤醒 |
| decision | 需要协调者决定方向 | 及时唤醒 |

非 complete 必须保留 unfinished 和 next_action，结果仍为 partial；不能满足 after 的成功依赖，不自动续做或重放。unfinished 只列委托范围内仍需执行的工作；未验证假设、被环境阻塞的测试和范围外检查写入 verification_gaps，保留可见性但不阻止范围交接。工作区、工具权限与审批规则不变。outcome 不认证验证通过或成果正确。摘要被截短时保留调度元数据，完整证据仍通过 canonical 报告或 task_result 读取。

新建 task_submit 默认 review_policy=batch；显式 immediate 保留逐阶段决策，resume 省略时沿用保存策略。未提供 outcome 的旧交接立即交付，不根据验证结果或风险推断调度。host 错误、取消和审批/问题等待不能被 worker 的 outcome 隐藏。用户输入仍优先。

图像 SSE 的内容完成与传输结束分别处理：`finish_reason` 固定内容，随后仍读取 usage，直到 `[DONE]` 或正常 EOF；HTTP 请求 deadline 统一管理超时，解析器不关闭连接、不设置尾部宽限定时器。未正常结束的请求返回错误，不以已收到的部分内容冒充成功。

### Shell 间接执行的支持边界

权限检查支持明确列出的 `find` 谓词和 `xargs` 参数；可静态确定的间接执行仍需整条命令的精确审批，嵌套硬拒绝规则保持优先。不支持的参数、GNU 长选项缩写、动态可执行文件名及 xargs 替换模式（`-I`、`-i`、`--replace`）直接拒绝，精确审批和自动判断均不能覆盖。此时改用可检查的直接命令或内置文件工具；权限检查不尝试实现完整命令解释器。文件、网络和进程权限仍由沙箱独立约束。
