# 更新日志

## Unreleased

- 子 Agent 改为 `profile + skills + prompt` 组合：内置仅保留可写 `coder` 与严格只读 `explore`，验证、调研和诊断由 task-scoped skill 表达；profile 白名单在工具暴露和执行前双重检查，子 Agent 的写前读授权按执行上下文隔离。
- `index` 新增 `callers:<symbol>` 和 `callees:<symbol>` 调用图查询。
- Runtime 的工具与子 Agent 回调统一为 `RunHost.Step(protocol.StepEvent)`，删除重复的 `ToolEvent`、`SubAgentEvent` 和中间转换层；自定义 Runner 需要改用 `StepEvent` 上报对应动作。
- Runtime 交互协调器由含义宽泛的 `runtime.Manager` 重命名为 `runtime.Runtime`。

### 兼容性说明

- 子 Agent Go API 为有意的不兼容升级：`AgentType` 改为 `Profile`，`RunConfig` 使用 `Profile/SkillContents`，`TaskRunner` 改为接收 `TaskSpec`，`Skill.AgentType` 改为 `Skill.AgentProfile`；task JSON 参数由 `type/thoroughness/prompt` 改为 `profile/skills/prompt`。自定义集成需同步迁移，不保留旧角色 shim。

## v0.4.3 - 2026-08-09

### Append-only 上下文与压缩

- 动态环境、Todo 和运行时策略不再改写 system 消息，改为带替代语义的 tagged user 消息按变化追加；hints 独立注入，不夹带其他运行时状态。
- 上下文压缩统一为全量摘要替换，移除没有独立行为的 Warning、MicroCompact、Compact 和 Blocking 分级；新增 `auto_compact_percent`，默认在模型窗口使用率达到 80% 时触发。
- 压缩轮次识别会忽略 runtime context、hint 和 rewind 隐藏事件，但 rewind 事实仍作为摘要证据保留。
- `/new` 创建全新会话且不继承旧上下文；删除语义重叠的 `/clear`。`/rewind` 只回滚文件，并向模型追加包含文件、动作和受影响目录的隐藏工作区事件。

### LLM 调用日志与用量展示

- calllog 逐次记录 LLM 请求，包含耗时、TTFT、模型、协议、请求和生效的 reasoning effort、输入、缓存命中、未缓存、输出和 reasoning tokens。
- 日志不保存 prompt、请求体、响应正文、凭据或完整 provider fingerprint；URL 会移除认证信息、路径和查询参数，错误只保留安全分类。
- TUI 每轮结束后展示本轮总量、输入、缓存、未缓存、输出、可选 reasoning 和缓存命中率；只执行命令的运行不生成 turn telemetry。
- TUI 顶栏展示平均缓存命中率、当前上下文占用、距压缩门限的空间和工作区增删统计，不再显示模型名和累计 token 数。

### 模型推理与上下文配置

- 新增模型能力驱动的 `reasoning_effort`，支持 `/effort` 命令切换；可选值根据当前模型解析，未知模型只提供 Auto，明确支持关闭推理的模型额外提供 Off/`none`。
- OpenAI 兼容与 Anthropic provider 统一 reasoning/thinking 配置、流式 reasoning 内容和 usage 解析；reasoning 配置使用独立的 provider-neutral 快照。
- reasoning 原文保存在 session 中供展示和恢复。DeepSeek 只回放工具调用轮次，Anthropic 会回放带签名的 thinking，包括 signature-only block；普通历史推理不进入后续模型请求。
- 模型上下文窗口、effort、thinking 和 replay 能力合并为单一 profile 数据源，并修正 DeepSeek V4、GPT-5.4/5.5 和 Claude Opus 4.5 的能力定义。
- 上下文窗口改为模型属性：模型显式 `context_window` 优先，其次使用内置模型元数据，未知模型回退到 128K。
- GUI 概览只读展示当前模型的有效窗口；模型卡片支持可选窗口覆盖。修改 provider、model 或 protocol 后会重新解析窗口和 effort，保存时不会固化自动默认值。

### 命令与交互体验

- `/model`、`/permission`、`/effort` 等状态切换命令不再把命令文本或冗余响应投影进会话；命令菜单与真实命令注册表统一。
- 优化 TUI 状态栏的 Model/Effort 顺序、配色、工作区差异格式、窄屏降级和 telemetry 中文指标名称。
- 系统消息改为左侧细线样式并保留正文空行；GUI 配置页同步支持压缩门限、推理强度和模型级上下文窗口。

### 可靠性与工程结构

- OpenAI 兼容流收到 `[DONE]`、Anthropic 流收到 `message_stop` 后立即结束，不再等待服务端关闭连接，避免回答完成后 ProcessingItem 短暂卡住。
- 发布工具链升级到 Go 1.25.12，纳入标准库安全修复；源码构建的最低 Go 版本同步调整为 1.25.12。
- 恢复 session 时保留当前模型的上下文窗口并重置 provider token 校准，避免旧模型窗口延迟压缩；取消运行会在 usage 收口后发布最终 telemetry，reasoning 也会恢复到 TUI/GUI 历史视图。
- checkpoint rewind 结果统一以结构化 `Changes` 为事实来源，文件缓存失效、用户文案和隐藏事件共用同一数据。
- `BuildRequest` 改为完整 `ModelRequest`，calllog 直接接收规范化 `StreamUsage`；删除 token meter、Todo 和 TUI 渲染中的重复状态与兼容分支。
- reasoning 设置下沉到独立中立包；TUI header/telemetry 改为 profile 驱动的统一渲染器。
- 修复 hints 在截断或恢复后的投影去重、命令运行复用上一轮 telemetry、模型重命名丢失 active/flash 引用，以及 Wails 外层绑定缺失导致生成的前端 API 不完整等问题。
- 增加中英文 README、贡献指南、安全策略、行为准则、治理与支持说明；CI 新增 GUI 与官网检查、ShellCheck、文档链接检查和依赖漏洞扫描，Release 发布校验和、SBOM 与构建证明。

### 兼容性说明

- 删除 `/clear`，请使用 `/new` 创建全新会话。
- calllog JSONL 字段升级为 `total`、`in`、`cached`、`new`、`out`、`reasoning` 等新 schema；依赖旧字段的脚本需要同步调整。
- 顶层 `context_window` 不再作为可编辑配置保存；需要覆盖时请在对应的 `models[]` 项内设置。

## v0.4.2 - 2026-08-04

### 前缀缓存修复（长对话成本显著下降）

- 修复 OpenAI 兼容通道（DeepSeek 等）的前缀缓存失效：provider 不再把全部 system 消息合并上提到请求头部。实测旧行为会让缓存命中归零；修复后，Todo、hints 和环境块等易变内容保持在消息流尾部。
- 环境块从历史消息之前移到尾部，其变化不再使整段历史缓存失效。
- 技能名单在会话内冻结：中途加载技能不再重写缓存前缀，名单改在启动、`/new`、`/clear` 和恢复会话时重建；恢复后补渲染名单，修复“已加载”标记丢失。
- 缓存统计兼容 DeepSeek 扁平字段 `prompt_cache_hit_tokens` 和 `prompt_cache_miss_tokens`，服务端报告值优先于算术推导。

### 改进

- 上下文分层统一为 0 到 5 层编号：系统提示和技能、记忆、压缩存档、历史消息、环境块、待办和提醒。层号对应稳定性。
- TUI `/context` 用量条按系统、工具、待办、技能、剩余、缓存和子 Agent 分段上色；命令输出仍为纯文本，IM 侧不受影响。

## v0.4.1 - 2026-08-01

### 重点更新

- 重构 Shell 任务生命周期：短命令直接返回，长命令转为按会话托管的后台任务；新增 `process` 工具，支持事件式 `wait`、`watch`、`list` 和 `stop`，并提供硬超时、输出截断和可靠关闭。
- 加强会话与权限隔离：工作区授权由每个 Bot 独立管理，临时目录权限按会话隔离；切换、清理或删除会话以及变更沙箱权限前，会先停止归属该会话的托管进程。
- 改进上下文可靠性：稳定系统提示与动态环境信息分层注入，恢复旧会话时使用当前规则；中断运行不丢弃已经完整提交的消息和工具结果。
- 改进上下文压缩与子 Agent：强化摘要的提示注入边界、失败保留和合并策略，更新 executor、researcher 和 verify 的职责与验证要求，并让子 Agent 实时获取工作区环境。
- 完善工具 JSON Schema：支持 enum、array items、嵌套 object properties 和 required 字段；`question`、`todo_write`、`task` 及 MCP 工具获得结构化参数定义。
- 统一 OpenAI 与 Anthropic 的多段 system context 组装，并改善权限失败反馈、一次性授权持久化边界和运行时提示。

### 体验与安全修复

- TUI 命令建议按 Enter 后只补全命令并保留参数输入，不再直接发送未填写参数的命令。
- 优化 Shell/Process 工具记录的名称、摘要、长输出首尾展示和 ANSI 控制序列清理。
- 文件工具的读取、写入与预览统一使用请求所属的工作区权限，避免 Bot 或会话之间共享授权状态。
- `web_fetch` 忽略进程代理变量，确保目标地址经过实际连接 IP 的 SSRF 校验。
- 会话加载改为先验证、清理旧运行时资源，再激活新快照；沙箱权限变更前会先回收旧权限下启动的进程。

### 开发者注意

这是一个包含 Go API 破坏性变更的次版本更新：

- 内置工具注册入口改为 `catalog.NewToolbox`，调用方应在退出时调用 `Toolbox.Close`。
- 原 `shell` 的后台任务管理能力迁移到独立的 `process` 工具。
- `session.Manager.Resume` 拆分为 `Load` 与 `Activate`，用于在切换会话前执行可失败的资源清理。
- `Bot.NewSession` 和 `ShellTool.Shutdown` 现在返回错误；`prompt.Builder.Build` 拆分为 `BuildStatic` 与 `BuildEnvironment`。
- 原 package-level workspace 授权 API 替换为每个 registry 独立的 `workspace.Manager`。
- `todo_write.todos` 从 JSON 字符串升级为结构化数组参数。

### 运行时说明

- 未设置 `shell.timeout_ms` 的托管进程会持续运行，直到进程自行退出、调用 `process stop`、关闭所属会话或 NekoCode 退出。不确定任务生命周期时建议显式设置硬超时。

## v0.4.0 - 2026-07-31

- 引入 `runtime` 与 `interaction` 层，bot 核心与界面解耦。
- TUI 入口迁移至 `./cmd/tui`。
- 优化命令提示；修复会话清空导致 UI 输出被冲掉。

## v0.3.3 - 2026-07-12

### 权限与安全

- 新增沙箱，权限系统重构为 rules / workspace / 沙箱三层。

### GUI

- 新增 GUI 桌面端（Flatpak 打包）、skill 管理页面。
- 优化 edit 与 diff 预览。

### 工具

- edit 改为内容锚定。
- 新增 `question` 工具、MCP 管理。

### 稳定性

- 新增上下文压缩策略。
- 修复 shell 日志竞态（stdout/stderr drain）。
- 消息宽度跟随终端。

## v0.3.2 - 2026-06-21

- 版本发布，无用户可见变更。

## v0.3.1 - 2026-06-16

- TUI 样式升级（hashline）。
- 修复多处 bug；CI 构建路径修正。

## v0.3.0 - 2026-06-12

### 架构

- LLM 层拆包解耦，Agent 精简。
- Bot 集成 Plugin / MCP / Hook。
- Edit 工具重写。

### 新功能

- 新增 Plan 模式、Hook 机制与 hint 提示。
- 支持切换模型。
- 接入即梦文生图 API。
- 新增 cindex 代码索引。

### 改进

- 上下文管理优化，缓存命中提升。
- sqlite 改为纯 Go 实现，移除 CGO 依赖。

## v0.2.0 - 2026-05-11

### 初始发布

- TUI 客户端、bot 核心与 agent 循环。
- 工具：shell、文件读写、web_search，支持并行调用。
- 新增 subagent、任务拆解、todo 列表、skill 系统。
- 结构化记忆摘要、防幻觉设计。
- 项目正式命名为 NekoCode。
