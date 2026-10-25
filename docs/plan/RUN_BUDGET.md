# 主 Agent / Sub-agent 通用运行预算

> **主要读者：** NekoCode 项目开发者与维护者。本文规划累计 token 预算、步数预算、逐轮提醒和预算耗尽后的收尾行为。
>
> 更新日期：2026-10-26。本文只描述尚未实现的能力；完成后同步用户指南、Agent 指南、架构和宿主协议，再从规划目录移除对应内容。

## 目标

为主 Agent 和 sub-agent 提供同一套运行预算语义：

- 支持常规执行步数预算，默认 150 步；保留 sub-agent Profile 的 max_steps 覆盖能力。
- 支持可选的累计 token 预算，统一限制主 Agent 和单次 sub-agent attempt。
- 每次 Agent 模型请求前，以 request-only 提示告知总额、已用量和剩余量。
- 步数耗尽时强制收尾：主 Agent 生成无工具总结，sub-agent 只提交结构化 handoff。
- token 耗尽时停止新的普通模型调用，保留已有输出和明确的 token_limit 终止原因，不静默结束。
- 预算限制与运行指标、TUI/headless 摘要使用同一份真实计数，不伪造 usage。

## 非目标

- 不引入 USD、价格表或供应商费用模拟。
- 不把单次上下文窗口占用当作累计 token 消耗。
- 不用步数推算 token，也不用 token 推算步数。
- 首期不实现父 Agent 与多个 sub-agent 共享一棵树的总 token 池；各 run/attempt 独立记账，父级现有汇总仅用于观测。
- 首期不跨 resume 累计预算；resume 创建新 attempt，并使用新预算。
- 不改变工具权限、审批、并发上限和递归限制。

## 术语与边界

三种预算必须保持独立：

| 类型 | 含义 | 耗尽后的行为 |
| --- | --- | --- |
| 步数预算 | 常规 Agent 执行轮次，例如 150 | 进入一次受控收尾，不再执行普通工具 |
| 累计 token 预算 | 当前 run/attempt 内实际 LLM 请求的 prompt + completion 累加 | 不再发起普通模型调用，返回已有结果和 token_limit |
| 单次上下文窗口 | 单次请求的消息、工具定义和输出预留能否放入模型窗口 | 沿用现有 compaction / RequestBudgetError |

自动 compaction 只降低后续上下文占用，不减少累计 token 消耗。强制收尾请求也消耗 token，不能成为绕过 token 预算的无限额外调用。

累计 token 采用“调用前准入、调用后结算”：预算只保证不会在已耗尽时继续发起普通调用，最后一次已获准请求可能使实际总量超过配置值。结果必须报告真实总量和超出值，不宣称这是不可越过的实时硬上限。

## 配置设计

新增通用执行层配置，不让 loop 自行读取全局 JSON：

~~~go
type Limits struct {
    MaxSteps  int   // 0 使用默认 150
    MaxTokens int64 // 0 不限制累计 token；负数无效
}
~~~

装配规则：

- 主 Agent：由全局配置注入，默认 MaxSteps=150、MaxTokens=0，保持未配置 token 预算时的兼容行为。
- sub-agent：由 RunConfig 注入同一结构；Profile 继续支持 max_steps，新增可选 max_tokens。
- Profile 的 max_steps 保持 1–200 校验；省略或为 0 时继承默认值。
- Profile 的 max_tokens 必须非负；省略时继承 sub-agent 默认值，显式为 0 表示该 attempt 不限制 token。解析结构需保留字段是否出现的信息（例如使用指针），不能只用 int64 同时表达这两种语义。
- 主 Agent、sub-agent、host/headless 的配置名称和继承规则必须在文档中保持一致。
- 预算按单次 Run/attempt 创建，不写入稳定 system prompt，不跨会话隐式共享。

## 共性抽象

新增小包，例如 bot/agent/runbudget，只负责预算状态、usage 记账、准入判断和提示快照，不负责消息组装或最终答案：

~~~go
type Snapshot struct {
    UsedSteps       int
    RemainingSteps  int
    UsedTokens      int64
    RemainingTokens int64
    TokenLimited    bool
    UsageEstimated  bool
}

type Decision int

const (
    Allow Decision = iota
    StepLimit
    TokenLimit
)
~~~

建议接口职责：

- Check(usedSteps)：在普通模型调用前返回 Allow、StepLimit 或 TokenLimit。
- RecordUsage(types.StreamUsage)：记录一次完整 provider usage，并保留 reported/estimated 状态。
- Snapshot(usedSteps)：生成提示、事件和结果共用的不可变快照。
- Reminder(usedSteps)：生成一次请求专用的预算提醒。

步数仍由各 loop 的权威计数提供，新包不得再次 step++。token ledger 只记录本 run/attempt；如未来支持共享预算，再单独设计并发扣减和父子作用域。

## 步数语义

- 一个常规步是一次 Agent 模型响应及其随附工具批次；自然文本回答正常结束，不为了计数继续循环。
- 普通工具批次执行后递增现有步数；sub-agent 完成协议重试继续计入常规步数。
- 步数耗尽后的强制收尾不恢复普通执行，也不执行普通工具。
- 强制收尾最多一次；失败或仍未提交时立即走确定性 fallback，不能通过完成协议重试绕过上限。
- 主 Agent 和 sub-agent 的 step_count 都报告真实常规步数，不把额外收尾伪装成普通一步。

## 逐轮提示注入

在主 Agent modelRunner 和 sub-agent reason 组装 ctxmgr.ModelRequest 时，把预算提醒加入 ExtraMessages。不要使用持久 user 消息或 SetHints，避免污染会话历史、压缩结果和稳定 system prompt 前缀。

提示至少包含：

~~~text
[Run budget]
Steps: 37/150 used, 113 remaining.
Tokens: 48210/200000 used, 151790 remaining.
Plan to finish within the remaining budget. Do not wait for the final step to summarize.
~~~

行为要求：

- 每次普通 Agent 模型请求重新计算，不重放旧快照。
- 未配置 token 上限时只提示步数，不显示虚假的 token 剩余量。
- 剩余步数较少时增加明确预警，要求缩小范围、保留证据并准备总结；阈值固定并可测试，例如 10 步。
- token 余量较低时要求停止扩展调查并优先产出结论；提示只是协作机制，真正的停止仍由执行层准入判断保证。
- 提示自身计入单次上下文窗口预算，但不重复写入持久 transcript。
- compaction、重试和强制收尾使用最新预算快照；内部 compaction 模型无需接收 Agent 规划提醒。

## token 记账

以 provider 完整响应的 StreamUsage 为权威来源，累计 PromptTokens + CompletionTokens。不要使用 Context Status.Tokens 或流式增量代替完整请求账单。

必须覆盖且只记一次：

- 主 Agent 普通模型调用及重试。
- 主 Agent 最终 synthesize。
- sub-agent 普通调用、完成协议重试和最终 submit_result 收尾。
- 主 Agent 与 sub-agent 各自发起的 compaction 调用。

实现时在现有 usage recorder 上 fan-out 到预算 ledger，保留 Metrics 原语义。父 Agent 汇总子 Agent usage 只用于观测，不能再记入子 Agent 自己的预算，也不能在独立预算模式下重复扣减父 Agent 预算。

Provider 未返回 usage 时不得按 0 处理并变相无限执行：使用现有 token 计数能力对本次请求和响应做保守估算，将快照和结果标记为 estimated；一旦同次请求获得真实 usage，不得再叠加估算值。

## 耗尽与强制收尾

### 步数耗尽

复用现有两条收尾契约：

- 主 Agent：进入现有无工具 synthesize，基于已有上下文输出最终总结。
- sub-agent：进入现有 finalizing，只暴露 nekocode_submit_result，要求填写完整 handoff 和 unfinished。
- 两者都发送 step_limit，保留最后文本、工具证据和真实预算快照。
- 收尾失败时主 Agent 使用已有最后文本及明确的预算提示兜底；sub-agent 返回带原因的 partial，主窗口必须可见。

### token 耗尽

- 在每次普通模型调用前检查；余量为 0 或负数时返回 token_limit。
- 若仍有 token 余量，步数收尾可以执行，并继续记录本次 usage；若 token 已耗尽，不再为了总结发起额外收费请求。
- 主 Agent 优先保留已接受的 final text，其次使用最后有效文本，并附加明确的 token 预算耗尽说明；不得只返回空正文。
- sub-agent 使用已有最后文本构造确定性 partial，说明未完成项和预算原因；不能伪造模型已提交的结构化 handoff。
- token_limit 与 step_limit 分开报告；同一次运行同时触及两者时，记录实际首先阻止下一次普通调用的原因，并保留完整预算快照。

## 代码接入点

- 主 Agent 步数准入与总结：bot/agent/loop.go。
- 主 Agent 请求组装与 usage：bot/agent/model.go、bot/agent/agent_state.go。
- sub-agent 步数准入与结构化收尾：bot/agent/subagent/subagent.go。
- sub-agent 请求组装与 usage：bot/agent/subagent/subagent_engine.go。
- request-only 消息：bot/contextmgr/contextmgr_context.go 的 ModelRequest.ExtraMessages。
- 配置与装配：bot/config/config.go、bot/core/bot.go、bot/core/bot_subagent.go。
- sub-agent Profile：bot/extension/agentprofile/profile.go、load.go。
- 终止摘要和宿主协议：protocol、runtime、headless 中现有 RunSummary/StopReason 路径。

## 实施阶段

### 阶段一：通用步数预算与逐轮提醒

1. 新增 runbudget 的 Limits、Snapshot、步数准入和 Reminder。
2. 主 Agent 固定 150 改为注入配置；sub-agent 保留 Profile 覆盖。
3. 两边通过 ExtraMessages 注入逐轮提醒。
4. 复用现有步数耗尽总结，不改变 token 行为。

### 阶段二：累计 token 预算

1. 在现有完整 usage 回调上接入独立 ledger，覆盖普通请求、重试、compaction 和收尾。
2. 增加 token 准入、估算降级和真实/估算标记。
3. 实现 token_limit、确定性 fallback 和可见的 partial/最终输出。
4. 接入全局配置、sub-agent Profile 与 host/headless 装配。

### 阶段三：协议、文档与收尾

1. 在运行摘要中暴露真实步数、token 用量、预算来源和终止原因。
2. 更新用户指南、Agent 指南、架构和正式流式协议。
3. 完成验收后移除本文对应条目，并同步收窄 STREAM_JSON_GAPS.md 中“可配置运行预算”的剩余范围。

## 验收与测试

- 主 Agent 和 sub-agent 使用同一预算包，但保留各自最终输出契约。
- 每次模型请求看到准确的最新剩余步数/token；提醒不写入持久历史，compaction 后不重复出现。
- 小步数预算能触发主 Agent 无工具总结和 sub-agent 仅 submit_result 收尾，普通工具不再执行。
- 高步数 + 低 token 能触发 token_limit；高 token + 低步数能触发 step_limit，证明两个维度独立。
- 普通请求、重试、compaction、主 Agent 合成和 sub-agent 收尾均只记一次。
- token 已耗尽时不再产生新的普通 LLM 调用；最后一次获准调用造成的越界被真实报告。
- Provider 缺失 usage 时进入估算并明确标记，不按 0 消耗处理。
- 未配置 max_tokens 时保持现有不限累计 token 的行为。
- 并行 sub-agent 各自独立记账，互不扣减；父级观测汇总不造成重复扣减。
- resume 新 attempt 重置预算；旧记录仍只读兼容。
- TUI、历史投影和 headless 在两种预算耗尽时均有非空、可诊断输出，step_count 和 StopReason 不伪造。
- 使用可控假模型和并发测试，不依赖真实模型请求完成验收。
