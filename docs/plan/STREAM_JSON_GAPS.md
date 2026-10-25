# NekoCode Headless 协议：实现状态与剩余差距

> **主要读者：** NekoCode 项目开发者与维护者。规划协议后续工作；对外接入以正式协议文档为准。

> 更新日期：2026-10-25
> 当前协议：`nekocode-headless/2`
> 正式契约：[STREAM_JSON.md](../developers/STREAM_JSON.md)
> 本文记录剩余设计项，不承诺实现时间。
>
> 完成本文某一项后：先修正 [正式契约](../developers/STREAM_JSON.md) 对应章节，再把该项从下文移除或标记完成。

## 剩余独立阶段

以下能力尚未通过 headless 协议提供，因此本文暂不删除。

| 能力 | 已有基础 | 需要补齐 |
|---|---|---|
| 统一后台任务模型 | shell 已有按会话托管的后台进程；process 工具提供 list/wait/watch/stop；子代理已有稳定 ID、queued/running/终态状态和 attempt | 统一跨 shell/子代理的任务 ID 与视图、单任务取消、turn 结束后的事件排空语义 |
| 队列优先级 | FIFO 排队与取消排队输入 | 插队规则、饥饿保护、与取消/关闭的交互；目前非 `next` 的 priority 被拒绝 |
| 连接级工具限制 | 工具目录与现有权限检查 | 每连接的允许/禁止工具集，以及对子代理和扩展工具的一致约束 |
| 可配置运行预算 | 代理已有内部步数上限及 step_limit 终态 | 宿主配置入口、校验和明确的预算作用范围；不引入 USD 预算 |

完成这些阶段时，应同步更新正式契约和测试，再决定是否删除本文及其引用。

## 暂缓的可选能力

这些是研究候选项，未纳入已确认的核心范围：

- NekoCode 自有 MCP server 的动态增删、启停与重连。
- 图像/文档等多模态输入，以及外部 tool_result 输入。
- 只追加历史而不触发运行、指定子代理投递。
- JSON Schema 约束的结构化输出与重试策略。

## 协议取舍

- 使用 NekoCode 自有版本、方法名和运行状态，不承诺 Claude 字段级兼容。
- 保留跨 provider 的合成内容流，不模拟 Anthropic 原始事件或 thinking signature。
- 不伪造费用、供应商 usage 或缺失的步数。
- 不接受宿主注入 Hooks、SDK-hosted MCP、agents、skills 或 system prompt。
- 工具审批不允许替换参数或写入权限规则。
- 不直接复制缺少底层状态保证的 task/workflow schema。

## 接入文档

- [STREAM_JSON.md](../developers/STREAM_JSON.md)：当前线协议契约。
- [headless/README.md](../../headless/README.md)：CLI 与 Go 宿主接入指南。
