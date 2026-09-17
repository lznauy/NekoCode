# NekoCode Headless 协议：实现状态与剩余差距

> 更新日期：2026-09-16
> 当前协议：`nekocode-headless/2`
> 正式契约：[STREAM_JSON.md](STREAM_JSON.md)
> 本文记录剩余设计项，不承诺实现时间。

## 已完成的核心范围

本轮确认范围为 P0/P1、运行中引导、显式关闭，以及协议与文档统一。

| 能力 | 当前实现 |
|---|---|
| 能力与服务器信息 | `initialize`、`server.info`、每轮 `system/init`；提供实际能力、工具目录和命令清单 |
| 模型管理 | `models.list`、`model.set`；空闲时修改当前运行环境的模型，不写全局默认配置 |
| 会话管理 | `sessions.list`、`session.history`、`session.new/resume/delete`；历史支持分页 |
| 权限控制 | `permissions.set` 切换完全访问开关；要求空闲 |
| 检查点回滚 | `workspace.checkpoints`、`workspace.rewind`；按 NekoCode 检查点恢复 |
| 运行结算 | 真实 `step_count`、`stop_reason`，区分完成、执行错误、取消和步数上限 |
| 审批拒绝摘要 | `result.permission_denials`；覆盖审批 broker 的拒绝/过期记录 |
| 子代理模型输出 | `subagent` 完整消息和可选增量，携带子代理与运行归属 |
| 运行中引导 | `run.steer`；仅在 runtime 允许引导的运行状态接受 |
| 显式关闭 | `shutdown/drain` 与 `shutdown/cancel`，清理并排空输出后退出 |
| 只读扩展发现 | `extensions.list`；不暴露进程参数、环境变量或凭据 |

模型、权限、会话和回滚修改要求当前运行与输入队列均已清空。
能力声明按实际后端服务生成：模型目录与当前模型查询分开，会话读操作与各写操作分开。
能力标识不等于可调用方法；字段、错误和时序以正式契约为准。

## 剩余独立阶段

以下能力尚未通过 headless 协议提供，因此本文暂不删除。

| 能力 | 已有基础 | 需要补齐 |
|---|---|---|
| 统一后台任务模型 | shell 已有按会话托管的后台进程；process 工具提供 list/wait/watch/stop；子代理已有独立事件 | 稳定任务 ID、进度、权威终态、单任务取消、turn 结束后的事件排空语义 |
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

- [STREAM_JSON.md](STREAM_JSON.md)：当前线协议契约。
- [headless/README.md](../headless/README.md)：CLI 与 Go 宿主接入指南。
