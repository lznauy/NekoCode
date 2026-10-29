# Hook 使用与开发指南

> **主要读者：** 用户侧扩展作者；第八节另面向 Go 接入开发者。修改核心 Hook 引擎时再参考架构文档。

本文面向想给 NekoCode 增加自动检查、运行提示或诊断记录的使用者，也介绍如何在 Go 应用中接入 Hook。示例基于当前 v1 插件协议。

第一次使用，可以先完成第二节的本地插件，再按需要阅读后续内容。字段和兼容规则的完整说明见 [Hook 插件协议](../../bot/policy/plugin/README.md)，核心治理边界见 [Policy 原则](../../bot/policy/README.md)。

## 一、Hook 能做什么

Hook 是在指定时机运行的一段代码。例如，工具即将执行时检查参数，工具失败后给模型补充提示，或在一次运行结束时记录诊断。

一个插件 Hook 由三部分组成：

| 部分 | 回答的问题 | 示例 |
| --- | --- | --- |
| 事件 | 什么时候运行？ | `PreToolUse`：工具执行前 |
| 条目 | 哪些调用需要检查，失败后怎么办？ | `matcher: "write|edit"`、`mode: "guard"` |
| 动作 | 执行什么代码？ | 一个 JS 文件，或一条外部命令 |

动作读取事件，返回提示、控制决定或私有状态更新。JS 和外部命令使用相同的事件与结果字段。

提示会影响模型下一次响应，但不授予工具权限。拒绝会阻止当前动作；结束决定会终止当前运行。Hook 命令本身会作为本机进程执行，安装插件前应检查其代码。

## 二、创建第一个 Hook 插件

这个示例拒绝 `write`、`edit` 工具修改文件名恰好为 `.env` 的目标，并给出原因。它演示结构化参数检查，不覆盖 Shell、其他工具或其他文件名，也不替代文件系统权限与沙箱。

### 1. 准备目录

在终端中创建：

```text
my-hook-demo/
├── .claude-plugin/
│   └── plugin.json
├── hooks.json
└── hooks/
    └── protect-env.js
```

在 `.claude-plugin/plugin.json` 中声明插件与 Hook 配置位置：

```json
{
  "name": "my-hook-demo",
  "version": "0.1.0",
  "description": "Hook 教学示例",
  "hooks": {
    "source": "hooks.json"
  }
}
```

`source` 相对于插件根目录，不能写成相对于 `.claude-plugin/` 的路径。

### 2. 声明触发条件

在 `hooks.json` 中填写：

```json
{
  "version": 1,
  "PreToolUse": [
    {
      "id": "protect-env",
      "mode": "guard",
      "matcher": "write|edit",
      "hooks": [
        {
          "type": "js",
          "path": "hooks/protect-env.js",
          "timeout": 1000
        }
      ]
    }
  ]
}
```

这里有两处容易混淆：

- `PreToolUse` 下的数组是多个独立条目；每个条目都有自己的 ID、状态和失败策略。
- 条目内部的 `hooks` 数组是多个动作，按顺序执行并合并结果。

`matcher` 完整匹配工具名，`write|edit` 只匹配这两个工具，不匹配 `rewrite`。省略或写空字符串会匹配全部工具。它使用 Go 正则语法，只有工具前后事件支持具体工具匹配器。

`timeout` 的单位是毫秒，`1000` 表示一秒。`id` 建议显式填写，方便在日志里找到同一条规则。

### 3. 编写检查代码

在 `hooks/protect-env.js` 中填写：

```javascript
function hook(event) {
  var path = String(event.args.path || "");
  var filename = path.split(/[\\/]/).pop();
  var count = (event.state.ints.checked || 0) + 1;
  var result = {
    state_patch: {
      ints: { checked: count }
    }
  };

  if (filename === ".env") {
    result.decision = {
      action: "reject",
      reason: "此规则禁止 write/edit 修改 .env，请改用示例配置文件。"
    };
  }

  return result;
}
```

脚本通过 `event.args` 读取结构化参数，用 `state_patch` 更新检查次数。普通文件没有控制决定，继续执行；`.env` 返回 `reject`，工具不会执行，拒绝原因反馈给模型。

JS 每次调用都创建新的虚拟机，不能靠全局变量保存上一次调用的计数。跨调用的临时状态要放进 `event.state`。

### 4. 安装并确认加载

从能访问 `my-hook-demo` 的项目目录启动 NekoCode，在输入框中执行以下斜杠命令；这些不是终端 Shell 命令：

```text
/plugin install ./my-hook-demo
/plugin info my-hook-demo
/plugin list
```

本地安装会把插件复制到用户插件目录 `~/.nekocode/plugins/`。安装过程会尝试激活插件；如果失败，先检查 `/plugin info my-hook-demo` 显示的加载错误，修复后重新启用。

让模型尝试通过 `write` 或 `edit` 修改一个测试用 `.env` 文件，可以观察拒绝结果。只有实际调用这两个工具时才会触发；模型直接回答、使用 Shell 或委托子 Agent，不能作为这个示例规则的触发验证。需要不调用模型的确定性验证，见第八节。

### 5. 修改后重新加载

加载时会编译 JS 并保留脚本快照，保存文件不会自动更新正在使用的 Hook。

如果修改的是已安装副本，可在任务结束后执行：

```text
/plugin disable my-hook-demo
/plugin enable my-hook-demo
```

也可以重启应用。若修改的是最初的 `./my-hook-demo` 源目录，应重新执行安装，把新版本复制到安装目录。再次对已启用插件执行 `enable`，不能代替禁用后启用。

## 三、选对事件和失败策略

当前有 16 个内部触发点，对应 17 个配置事件名。`PostToolUseFailure` 只是工具执行后事件的失败过滤视图。

| 配置事件 | 适合做什么 | 使用时注意 |
| --- | --- | --- |
| `SessionStart` / `SessionEnd` | 会话开始提示、结束诊断 | Start 每个注册条目在当前会话执行一次；End 是正常生命周期清理，不保证进程崩溃时执行 |
| `RunStart` / `RunEnd` | 单次运行开始、结束记录 | 一次会话可以有多个 run；取消、失败也有结束通知 |
| `UserPromptSubmit` | 用户输入进入 run 后检查 | 拒绝会直接结束当前 run 并返回错误 |
| `PreModel` | 请求模型前提供提示或检查 | 包括最终兜底合成请求；拒绝会结束当前 run |
| `PreToolUse` | 检查工具名及参数 | 拒绝当前调用，结束决定会阻断尚未执行的整个批次 |
| `PostToolUse` | 根据工具结果补充提示 | 包含成功、失败；已阻断工具不触发，不能撤销已发生的执行 |
| `PostToolUseFailure` | 只处理工具失败 | 与 `PostToolUse` 共享内部触发点 |
| `PostToolBatch` | 查看一批工具的汇总结果 | `batch` 包含成功、失败和被阻断的调用 |
| `PermissionRequest` | 记录即将交给用户的审批 | 仅观察；允许工具名 matcher，不能批准或拒绝 |
| `PermissionDenied` | 记录用户或权限规则的明确拒绝 | `args.source` 区分 `user` 与 `rule`；仅观察 |
| `Stop` | 检查最终回答 | 正常循环拒绝后有限重试；终止恢复路径不再重试 |
| `SubagentStart` / `SubagentStop` | 观察子 Agent 生命周期 | 不会让父插件自动接管子 Agent 的工具检查 |
| `PreCompact` / `PostCompact` | 观察上下文摘要尝试 | Post 也报告失败，不能直接当作摘要成功 |

新插件建议明确填写 `mode`：

| 模式 | 代码正常返回时 | 出错、超时或返回非法结果时 |
| --- | --- | --- |
| `guard` | 可以继续、拒绝或结束 | 拒绝当前动作 |
| `observer` | 只返回提示、诊断或状态更新 | 记录诊断，不因该条目失败而阻断动作 |

配置层面，`guard` 只允许用于 `UserPromptSubmit`、`PreModel`、`PreToolUse`、`Stop`。显式 `observer` 返回 `reject` 或 `stop` 会被当作错误。

未填写 `mode` 时，只有 `PreToolUse`、`Stop` 默认是 guard，其他事件默认按 observer 处理失败。为兼容已有插件，省略模式和显式 observer 的控制结果检查并不完全相同；新配置不要依赖这个差异。底层协议允许工具后事件返回结束决定，但不能拒绝已经执行完的工具动作。

结束通知使用独立的一秒清理预算，应保持简短。不要把必须成功的持久化事务仅放在结束 Hook 中。

### 观察审批与拒绝

`PermissionRequest` 在执行器实际调用审批回调之前触发，覆盖命令审批、命令与能力合并审批、运行后发现的额外能力审批，以及工作区访问扩展。规则放行、记住授权或 full 模式导致免询问时不触发；没有审批回调也不伪造一次请求。

`PermissionDenied` 在用户拒绝审批（包含关闭审批框）或权限规则明确拒绝工具/能力时触发。规则直接拒绝只产生 Denied，不会先产生 Request。Plan Mode、子 Agent Profile 的工具过滤、普通工具错误、缺少审批回调与取消执行本身不归类为此事件。取消、超时、关闭 Runtime 及无人工交互的适配器清理，通过内部取消标记与显式拒绝区分；该标记不在客户端 JSON 决策中开放。用户明确拒绝或关闭审批框仍产生 Denied，即使任务同时被取消。

两个事件的 `tool` 是 canonical 工具名；工作区扩展审批使用 `workspace`。`args` 包含：

| 字段 | 含义 |
| --- | --- |
| `call_id` | 原始工具调用 ID；同一调用发生多次独立审批时保持一致 |
| `source` | Request 为 `approval`；Denied 为 `user` 或 `rule` |
| `tool_args` | 该次审批或拒绝对应的工具参数快照，不含运行时回调 |
| `kind` | 实际审批请求的类型，规则直接拒绝时可能没有此字段 |
| `approval` | 审批范围，例如 capabilities、write_paths、outbound_domains；规则拒绝时只有预知的范围才会附带 |

Denied 的 `error` 为 true，`error_message` 为拒绝原因。`actor_id` 为 `main` 或子任务 ID；父级插件可以观察子 Agent 的审批事实，但不能通过这些事件接管其权限判断或借用父级工具检查的 ledger。每个真实审批产生一个 Request；只有被拒绝时再产生一个 Denied。

仅允许 observer 模式，返回 `reject`、`stop` 属于非法结果；返回内容不会变成授权决定或模型提示。插件可以记录诊断或更新条目私有状态。事件数据是独立快照，修改它不会修改执行参数、域名授权或用户看到的审批。拒绝通知在原 run 被取消后仍有至多一秒的观察预算。

例如，只为 Shell 的拒绝记录计数：

```json
{
  "version": 1,
  "PermissionDenied": [{
    "matcher": "shell",
    "mode": "observer",
    "hooks": [{
      "type": "js",
      "code": "({state_patch:{ints:{denied:(event.state.ints.denied || 0) + 1},strings:{last_source:event.args.source,last_actor:event.actor_id}}})"
    }]
  }]
}
```

协议继续使用 v1：本次只增加可选事件名，旧事件和载荷不变。旧版本不认识这两个配置名，会在加载时明确报错；没有配置新事件的旧插件保持原行为。

## 四、理解事件与返回值

配置中的事件名是 `PreToolUse`，传入脚本的 `event.event` 则是 `pre_tool_use`。不要在脚本里混用这两种写法。

常用字段如下：

| 字段 | 含义 |
| --- | --- |
| `event` | 内部事件名，例如 `pre_tool_use` |
| `tool`、`args` | 工具名和参数；生命周期事件中可能为空 |
| `error`、`output`、`error_message` | 工具结果、是否失败及错误文本 |
| `input` | 当前 run 的用户输入 |
| `intent`、`text` | 回答意图和当前候选回答 |
| `batch` | 工具批次结果 |
| `state.ints`、`state.strings` | 当前条目的私有状态快照 |
| `pluginRoot` | 当前插件的根目录 |
| `hook_id`、`action_index` | 条目身份及从 0 开始的动作序号 |
| `session_id`、`run_id`、`actor_id`、`evaluation_id` | 日志关联身份 |

例如，压缩和子 Agent 生命周期附加的 `status` 在 `args` 中，不能假设存在顶层 `event.status`。完整事件结构见 [共享类型](../../bot/policy/contract/types.go)。

新代码使用 `decision` 和 `hints`，不必使用旧的 `block_tool`、`require_tool` 等兼容字段：

```json
{
  "decision": {
    "action": "reject",
    "reason": "请先修正参数后再试。"
  },
  "hints": [
    {
      "type": "workflow",
      "severity": "info",
      "content": "使用示例配置文件验证改动。"
    }
  ]
}
```

`decision.action` 只有三个值：

- `continue`：继续，也可以省略整个 `decision`。
- `reject`：拒绝当前动作。具体是否反馈模型重试，取决于上表中的事件。
- `stop`：结束当前 run。插件通常省略 `stop_reason`，默认按 `policy_blocked` 结束。

没有输出时，JS 可以返回 `null`。返回字符串会被当作提示，不会被当作拒绝决定，即使字符串内容看起来像 JSON。

同一结果只能有一个控制决定，不能把 `decision` 和旧控制字段混用。不同条目的结果统一合并，优先级为 `stop > reject > continue`；提示全部保留。结束和拒绝同时出现时，当前输出仍然被拒绝，不能由另一个“完成”决定覆盖。

## 五、调用外部命令

需要复用 Python 或其他现有程序时，可使用 `command`。下面的示例依赖本机 `sh` 和 `python3`，在 run 结束时输出一条提示并写入命令诊断。

把以下 `RunEnd` 条目加入 `hooks.json`，与已有 `PreToolUse` 并列，保留顶层 `version: 1`：

```json
{
  "RunEnd": [
    {
      "id": "report-run",
      "mode": "observer",
      "hooks": [
        {
          "type": "command",
          "command": "python3 \"$PLUGIN_ROOT/hooks/report.py\"",
          "output_format": "json",
          "timeout": 500
        }
      ]
    }
  ]
}
```

新增 `hooks/report.py`：

```python
import json
import sys

event = json.load(sys.stdin)
print("report-run: received " + event["event"], file=sys.stderr)
print(json.dumps({
    "hints": [{
        "type": "run_report",
        "severity": "info",
        "content": "运行结束，run_id=" + event["run_id"]
    }]
}, ensure_ascii=False))
```

命令以插件根目录为工作目录，通过 stdin 读取一个 JSON 事件。stdout 只写结果，调试信息写 stderr；不要往 stdout 混入启动日志。退出码非零按动作失败处理。

`PLUGIN_ROOT` 和 `CLAUDE_PLUGIN_ROOT` 是环境变量，Shell 中应像示例一样引用并加双引号。事件参数通过 stdin 传入，不要把它们拼接进 Shell 命令。

`RunEnd` 的提示用于结束阶段的观测，不会再触发一次模型请求。需要影响下一次模型响应的提示，应放在 `PreModel` 或合适的工具事件。

显式 `version: 1` 时，命令默认解析 JSON；没有版本的旧配置默认把 stdout 当作文本。新插件建议声明版本和输出格式。需要旧式文本提示时，写 `output_format: "text"`。

## 六、保存少量私有状态

第二节中的 `checked` 保存在当前条目的 `state.ints` 中；也可用 `state.strings` 保存短字符串。

状态有以下边界：

- 普通条目在下一个 run 开始时清空；`SessionStart`、`SessionEnd` 条目保留到会话切换。同一会话后续 run 不会再次执行已完成的 SessionStart 条目。
- 状态只属于当前条目，不在不同条目之间共享；多个动作的 patch 在合并后提交，后一个动作不会自动读到前一个动作尚未提交的 patch。
- 一个条目中的动作发生错误时，该条目的候选状态不提交。其他条目已经提交的状态不会一起回滚，命令产生的外部副作用也不会回滚。
- 这是内存状态，不能用来替代数据库、文件持久化或工具授权记录。重新加载条目会重置其状态。

每个条目最多保存 256 项、64 KiB 的状态。返回 `state_patch` 是按键更新，不是替换整个状态，也没有删除键的协议字段。

## 七、排查没有生效或运行失败

先检查加载，再检查触发和执行：

| 现象 | 优先检查 |
| --- | --- |
| 安装后看不到效果 | `/plugin info my-hook-demo`、`/plugin list` 是否报告加载错误，插件是否启用 |
| 改了 JS 仍执行旧逻辑 | 改的是源目录还是安装副本？是否重新安装或重新加载？ |
| 工具没有触发 | 实际工具名是否匹配？是否走了子 Agent 或其他工具路径？ |
| 命令输出 JSON 却只显示为文字 | 是否声明顶层 `version: 1` 或 `output_format: "json"`？ |
| guard 突然拒绝 | 检查语法、返回结构、超时、状态容量及命令退出码 |
| observer 返回了拒绝却没阻断 | 显式 observer 不允许返回控制决定；错误只进入诊断 |
| 两个动作的状态对不上 | 动作收到条目快照，patch 在整条目成功后提交 |
| Stop 拒绝后没有再生成 | 最终兜底恢复路径不重试，普通路径也只有有限重试 |

日志默认位于 `~/.nekocode/logs/`。可在终端检索：

```bash
rg 'hook_audit|hook_command|hook_resolved' ~/.nekocode/logs/
```

`hook_command` 记录命令耗时、错误及有界 stderr；`hook_audit` 记录回调结果、状态是否提交及用量；`hook_resolved` 记录事件合并后的控制决定。先用 `run_id` 定位运行，再结合 `evaluation_id`、`hook_id`（审计日志中写作 `hook`）和 `actor_id` 关联。同一事件的多个动作通过 `action_index` 区分。

诊断中出现 `discarded`，通常表示回调执行期间条目被替换或会话发生变化，旧结果因此未提交。内存中只保留最近 64 条审计记录；完整排查还需要结合落盘日志。

常见限额如下，完整边界见插件协议：

| 项目 | 当前上限或默认值 |
| --- | --- |
| 单动作超时 | 默认 5000 ms，最大 60000 ms |
| 单条目累计预算 / 单事件总预算 | 最多 60 秒 / 30 秒；父运行取消优先 |
| 每份配置条目数 / 每条目动作数 | 256 / 1–32 |
| 配置 / 单个 JS 源码 | 各 1 MiB |
| 命令 stdout / stderr / JS 结果 | 分别 4096 字节；结构化结果不能截断使用 |
| JS `readFile` | 插件目录内普通文件，最多 64 KiB |
| JSON 嵌套 | 最多 64 层；重复字段、未知字段、多个顶层值拒绝 |

JS 使用 Goja，不是 Node.js 环境；没有 Node 的 `require`、`process` 或文件系统模块。需要读取插件内文件时用 `readFile("相对路径")`，需要外部程序时改用命令动作。`output_schema` 只支持顶层 `type` 和 `required`，不能填写完整 JSON Schema 的其他约束。

## 八、在 Go 中验证与扩展

### 不调用模型验证插件

在仓库根目录新建临时文件 `hook_demo.go`，填写下面的完整程序。程序读取第二节的 `./my-hook-demo`，直接构造工具事件，不访问模型，也不会实际写文件：

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "os"

    "nekocode/bot/policy"
)

func main() {
    hooks, err := policy.LoadPluginHooks("./my-hook-demo", "hooks.json")
    if err != nil {
        panic(err)
    }
    gov := policy.New()
    for _, hook := range hooks {
        if err := gov.Register(hook); err != nil {
            panic(err)
        }
    }
    gov.SetSessionID("hook-demo")
    gov.ResetRun()
    for _, path := range []string{"config.example", ".env"} {
        results := gov.BeforeTool(policy.ToolRequest{
            Name: "write", Args: map[string]any{"path": path},
        }, context.Background())
        out := policy.Resolve(results)
        fmt.Printf("%s: %s\n", path, out.Action)
    }
    if err := json.NewEncoder(os.Stdout).Encode(gov.Inspect()); err != nil {
        panic(err)
    }
}
```

在仓库根目录运行 `go run hook_demo.go`。前两行应为：

```text
config.example: continue
.env: reject
```

最后一行是只读诊断快照，可查看 `hooks`、`counters`、`recent`；执行中查询还会显示 `active` 及耗时。`Inspect()` 不运行回调、不暴露私有状态值，也不会等待回调结束。该接口目前是 Go API，不是 `/hook inspect` 斜杠命令。

验证后移走临时文件，避免它和仓库已有的 `main.go` 一起参与常规构建。

### 注册 Go 回调

嵌入式应用还可以直接向自己的 `gov` 注册回调。下面片段可放在上例 `gov.ResetRun()` 之后：

```go
err = gov.Register(policy.Hook{
    Name: "request-note",
    Point: policy.PreModel,
    Run: func(ctx context.Context, s policy.State) (*policy.Result, error) {
        if err := ctx.Err(); err != nil {
            return nil, err
        }
        return &policy.Result{Hints: []policy.Hint{{
            Type: "workflow", Severity: "info",
            Content: "本次响应优先说明已确认的事实。",
        }}}, nil
    },
})
if err != nil {
    panic(err)
}
```

主 Agent 通过 `agent.Config.Policy` 使用这个 Policy。接入模型和工具的完整例子见 [hooks-guard 示例](../../examples/hooks-guard/main.go)；其中使用旧 `On`、`BlockTool` 字段，仍受兼容支持，新代码优先用 `Run`、`Decision`。

Go 回调只填写 `Run` 或 `On` 中的一个。`Guard: true` 控制出错时是否阻断，不会由 `Point` 自动推导；Go 的默认值与插件配置默认值不同。回调必须主动响应 context，进程内代码无法被安全强制终止，也不要从回调重入同一个 Policy 的评估或 `ResetRun()`。

## 九、新增规则前先确认边界

优先使用能根据结构化事实准确判断的规则，例如明确的工具名和参数约束。判断“任务是否完成”“测试是否充分”需要语义理解，不适合简单正则硬拦截。

父 Policy 观察子 Agent 生命周期，但不会复用父插件的工具 Hook。对子 Agent 的读取授权也不能由父 Agent 或兄弟 Agent 代替。新增触发点、调整核心控制能力时，应先阅读 [架构边界](../maintainers/ARCHITECTURE_BOUNDARIES.md) 和 [Policy 扩展约定](../../bot/policy/README.md)。
