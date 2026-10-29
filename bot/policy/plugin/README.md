# Hook 插件协议

插件 hook 使用 `contract` 中的 v1 事件和结果类型。JS 与命令的协议一致，旧字段通过兼容适配保留。

从创建插件、安装到调试的步骤见 [Hook 使用与开发指南](../../../docs/developers/HOOK_GUIDE.md)。

## 配置

```json
{
  "version": 1,
  "PreToolUse": [{
    "id": "protect-config",
    "mode": "guard",
    "matcher": "write|edit",
    "hooks": [{
      "type": "js",
      "path": "hooks/protect.js",
      "timeout": 1000
    }]
  }]
}
```

- `id` 在插件和事件内唯一；未填写时按配置内容生成稳定 ID，完全相同的条目按出现次数区分。需要跨修改追踪身份时填写显式 ID。
- `matcher` 是完整匹配的工具名正则，仅用于工具前后事件；空值匹配全部。分组与 `|` 均按 Go 正则语义处理。
- `mode: guard` 在错误、panic、超时或非法返回时拒绝当前动作。默认 PreToolUse、Stop 为 guard，其他事件为 observer。显式 observer 不允许返回控制决定。
- guard 仅可配置在 UserPromptSubmit、PreModel、PreToolUse、Stop。观察事件只能返回提示、诊断或状态更新。
- 一个条目的多个 actions 按顺序执行，合并全部提示与状态更新；遇到错误终止该条目并丢弃其未提交状态。已发生的外部命令副作用不能回滚。
- 命令的 `output_format` 可指定 `text` 或 `json`。未声明配置 `version` 时默认 text，保留旧 JSON 文本和截断提示；显式 `version: 1` 默认 json，非空 stdout 必须是合法结果对象。可在 v1 命令上指定 `output_format: "text"` 保留文本输出，不再猜测首字符。
- JS 脚本在加载时编译并保留快照，每次运行创建独立 VM；修改脚本后通过插件 reload 生效。
- `timeout` 单位为毫秒，默认 5000，最大 60000。单条目累计预算最多 60 秒，单事件总评估预算为 30 秒；父 run 取消优先。
- 未知字段、事件、类型，无效正则、JS 语法、文件路径、重复 ID 和超时范围在加载时拒绝。插件不会部分激活；CLI 和 GUI 展示加载错误。

## 事件

内部共有 16 个触发点，配置支持 17 个事件名，其中 PostToolUseFailure 是 PostToolUse 的失败过滤视图：

| 配置事件 | 触发时机 |
|---|---|
| SessionStart | 当前 session 首个 run 开始时，每个已注册条目一次；后续 run 保留其状态 |
| SessionEnd | 切换 session、重建 runtime 或关闭 Bot 时，活动 session 结束 |
| RunStart / RunEnd | 每次运行开始 / 结束，取消或失败也有结束通知 |
| UserPromptSubmit | 用户输入进入当前 run 后 |
| PreModel | 模型工具循环及最终兜底合成请求前 |
| PreToolUse | 单个工具执行前 |
| PostToolUse | 单个未被阻断的工具执行后，包含成功和失败 |
| PostToolUseFailure | 上述事件中失败的工具调用 |
| PostToolBatch | 工具批次处理后，batch 包含成功、失败和阻断结果 |
| PermissionRequest | 执行器实际调用审批回调前；仅观察，不能授权或阻断 |
| PermissionDenied | 用户或权限规则明确拒绝时；仅观察，source 区分 user/rule |
| Stop | 模型准备以文本结束本轮运行时，包括最终兜底合成及固定回退文本 |
| SubagentStart / SubagentStop | 真正开始执行的子 Agent 开始 / 结束，仅观察其生命周期 |
| PreCompact / PostCompact | 实际摘要尝试前 / 结束后，status 区分 completed 与 failed |

命令通过 stdin 读取一个 JSON 事件，stdout 返回 JSON 结果，stderr 用于诊断。JS 使用相同字段的 `event`，也可以定义 `hook(event)` 函数。

审批事件的 args 包含 call_id、source、tool_args，实际审批附带 kind、approval；范围与诊断说明见 [Hook 指南](../../../docs/developers/HOOK_GUIDE.md#观察审批与拒绝)。父级观察 Policy 接收子 Agent 的审批事件并保留 actor_id；Plan Mode、Profile 工具过滤、普通工具失败和没有审批回调不生成审批拒绝事件。新增事件继续使用协议 v1，旧运行时会拒绝不认识的新配置名。

事件字段包括 `version`、`event`、`hook_id`、`evaluation_id`、`action_index`、`session_id`、`run_id`、`actor_id`、`pluginRoot`、`input`、`tool`、`args`、`error`、`output`、`error_message`、`batch`、`intent`、`text`、`state`。没有对应事实的字段为空或省略。

`state.ints` 和 `state.strings` 是该条目的私有快照。默认在 run 开始时清空；SessionStart/SessionEnd 条目在切换 session 时清空。Once 标记保存在引擎内部，与插件状态分离。

每个 hook 状态最多 256 项（ints、strings 合计）、64 KiB。容量按 UTF-8 键和值的字节数统计，整数值按 8 字节计算。每次基于完整候选状态检查，超限时整次更新回滚，包括 Go 回调直接写入与 state_patch；按 guard/observer 的失败策略处理。状态 patch 提交后不再进入跨 hook 决策汇总，避免私有状态串联。

## 结果

```json
{
  "decision": {"action": "reject", "reason": "请先读取目标文件"},
  "hints": [{"type": "workflow", "severity": "info", "content": "读取后重试"}],
  "state_patch": {"ints": {"attempts": 1}, "strings": {}}
}
```

控制决定只有三种：

- `continue`：继续处理。
- `reject`：拒绝当前输入、模型请求、工具调用或最终回答。输入、模型请求的拒绝直接结束当前 run 并返回错误；工具拒绝反馈给模型；最终回答拒绝走有限重试，耗尽后返回错误，禁止将被拒绝文本保存成最终回答。
- `stop`：结束 run；工具前触发时阻断整个尚未执行的批次。默认 stop_reason 为 policy_blocked，并返回明确错误；可信内置规则可使用自身的结束原因。

每个结果最多一个控制决定。所有匹配条目顺序评估，统一汇总，控制优先级为 stop > reject > continue，同级合并原因，提示按注册顺序全部保留。拒绝事实独立累积：stop 与任意 reject 并存时，结束原因为 policy_blocked，不能保存当前回答；该状态在多动作和多 hook 的嵌套合并中保留。多个 stop 同时出现时，policy_blocked、interrupted、format_error 的优先级高于 completed。状态只在完整结果通过校验后提交；条目间各自提交，整个事件不是外部事务。

兼容字段 `hint`、`stop`、`block_tool`、`block_final`、`require_tool` 仍可使用，但不能与 decision 混用。旧 post-tool require_tool/block_final 转为提示。旧命令文本输出（包括 JSON 文本）、JS 字符串返回继续作为提示。命令是否解析结构化结果由 output_format/配置版本决定，与输出首字符无关。legacy BlockTool.Tool 的适用范围在动作合并前统一过滤，增加提示动作不会扩大门禁范围。

命令 stdout、stderr 分别有 4096 字节采集上限，结构化结果超限按失败处理，文本结果保留截断提示；JS 返回结果也限制为 4096 字节。Unix 使用独立进程组并在取消或退出时清理子进程；其他平台目前只保证取消直接进程。命令执行依赖 `sh`。

Go 回调使用 `Run(context.Context, State) (*Result, error)`；旧 `On(State) *Result` 仍兼容。Go 回调是可信进程内代码，必须主动响应 context，无法被强制安全抢占。引擎不会用遗留 goroutine 模拟超时；JS 与命令由各适配器实现可中断执行。回调不能重入同一引擎的评估或 ResetRun。

审计记录每次执行的 hook（包括没有返回结果的回调）、run/actor 身份、耗时、状态是否提交、状态项数与字节数；热替换或会话变化导致的旧回调记录为 discarded。内存中始终仅保留最近 64 条。默认触发描述只记录参数数量，避免把命令和文件内容写入日志；DescribeTrigger 可显式提供业务描述。

命令无论成功或失败均写入 hook_command 诊断记录，包含 hook/run/actor/action 身份、耗时、错误和有界 stderr；stderr 不混入 stdout 协议。hook_resolved 记录事件最终控制决定，summary 包含失败数。run/session 结束通知采用独立的一秒清理预算；会话切换会使旧回调的决定和状态失效。子 Agent 的工具授权仍使用独立 ledger，父 policy 只观察子 Agent 生命周期和审计结果。

配置和 JS 源码各限 1 MiB；每份配置最多 256 个 hook 条目。配置、脚本和 readFile 只读取普通文件，readFile 上限为 64 KiB。JSON 嵌套最多 64 层，重复字段（包括嵌套字段）、未知字段和多个顶层值均拒绝，避免解析覆盖造成含义不一致。

`output_schema` 只支持顶层 `type` 和 `required`，不是完整 JSON Schema 校验器。不支持的关键字、无效类型和重复 required 字段在加载配置时直接报错，不会静默忽略。

最终兜底合成同样经过 PreModel 和 Stop。这个终止恢复路径不再重试生成；Stop 提示只进入审计，明确拒绝会返回错误，不能退回固定文本绕过门禁。Hook 等待期间的用户追加指令按取消处理，正常推理可恢复，不会被误报为永久拒绝。

## 只读诊断

Go 调用方可使用 `snapshot := policy.Inspect()` 获取独立快照，可直接序列化为 JSON。快照包含注册条目及代次、超时、Once 完成情况、状态用量、当前执行和耗时、当前 run 计数及最近 64 条执行记录；不包含私有状态值。查询不会执行 hook，也不会等待正在运行的回调结束。

`evaluation_id` 在每个 run 内递增，结合 `run_id` 关联事件、命令动作和汇总决定；`session_id`、`actor_id` 标识执行上下文。`active` 为空表示没有正在执行的回调，不表示没有排队的事件。计数按回调统计，`rejections` 包含所有拒绝，`block_tools` 和 `block_finals` 分别只统计工具前与最终回答拒绝。ResetRun 清空计数，切换 session 清空旧诊断记录。
