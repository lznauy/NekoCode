# 架构边界与协作约定

> **主要读者：** NekoCode 项目开发者与代码审查者。新增或修改实现时遵守分层、状态和契约边界。

NekoCode 保持模块化单体。新增能力优先放入现有职责层，不通过新增服务或跨层调用绕开边界。

| 层 | 职责 | 不应承担 |
| --- | --- | --- |
| `protocol` | 无应用依赖的领域事件和值类型 | Runtime 生命周期、传输编码 |
| `bot/agent`、Provider、Tools、Policy、Context | 推理循环及工具执行 | UI、Runtime 和渠道状态 |
| `bot/core` | 标准产品的领域组装、会话和配置 | 渠道协议与消息投影 |
| `runtime`、`runtime/internal` | Run 生命周期、审批、事件、录制及应用操作 | 依赖具体 Bot 或交互适配器 |
| `runtime/standard`、`runtime/agentrunner` | 领域对象与 Runtime 的显式适配 | 成为底层模块的依赖 |
| ACP、headless、HTTP、TUI、GUI、Connector | 输入校验及协议投影 | 自行实现领域状态转换 |

`go test ./internal/architecture` 检查核心依赖方向，包括非当前平台源文件；该测试随 `go test ./...` 进入 CI。
新模块应先确定所属层与负责人，再定义输入、输出、生命周期及失败行为。现有 CODEOWNERS 的实际负责人保持不变，不预设尚未加入的协作者。

## 能力与状态所有权

- `Runner` 是 Runtime 唯一必选依赖。组合根通过 `New(runner, ServiceGroups)` 按领域显式装配能力，服务定义和构造入口各保留一套。
- 消费方通过 `RunControl`、`RunEvents`、`HumanInteraction`、`SessionManagement`、`ModelReader`、`ObservationReader` 组合所需能力，不为各适配器重复维护同一协议。
- 一个应用使用一个 Runtime，交互表面共享该实例。Bot 和 Agent 拒绝并发运行；这种保护不意味着支持多个 Runtime 同时管理同一 Bot 的配置、关闭和通知生命周期。
- Agent 的运行锁覆盖回调绑定及恢复。调用方取消在 run 初始化后绑定，避免 Reset 清除取消信号。手工 `BindOutput`/`BindInteraction` 仅用于实例闲置时的配置，不可在其他 run 中调用。
- Provider 默认设置在客户端创建时确定。内置 OpenAI/Anthropic 客户端支持 `RequestOptions` 覆盖单次请求的 token 上限和 thinking；新的共享客户端应采用单次请求配置，不修改共享实例状态。旧的自定义 `LLM` 接口保持兼容，但未实现可选 `RequestClient` 时不能使用单次覆盖。
- `session.Manager.Current` 返回深拷贝；`Activate` 和 `Save` 也复制传入快照。调用方修改快照不会隐式提交，需显式调用 `Save` 或 `Activate`。

## 会话设置

ACP 保存连接内的会话偏好；模型、推理深度和权限的组合应用由 `Runtime.ApplySessionSettings` 承担。一个 mutation 锁覆盖快照、应用和回滚，期间不能启动 run。
失败时恢复目标模型的推理设置和原活动模型；回滚失败与原错误一起返回。该操作不是数据库事务，不能保证失败的外部回调一定可恢复。新后端实现须遵守相同契约。
`acp.Backend` 增加了 `ApplySessionSettings(SessionSettings) error`；自定义后端需要实现原子应用与回滚，不能只把三个公开 setter 串起来冒充事务。

延迟结果恢复按整批替换后的净 token 增量检查预算，先验证全部结果，再统一替换并更新计数；缩短占位内容的负增量也必须计入。技能结果携带本地 `SkillName` 元数据，只有内容实际进入上下文后才更新加载标记，延迟结果在恢复时更新；交付回调在上下文锁外执行，该元数据不进入 provider 请求。

## 会话变更与用量恢复

恢复会话的 completion 总量是替换值，不是一次新的模型消耗。`Agent.RestoreSessionUsage` 在上下文恢复后替换总量并重置上一轮的用量基线；新建或删除当前会话时清零。模型切换引起的 Agent 重建保留会话总量，不把历史用量计入新一轮。

会话操作区分未提交失败与已提交后的收尾失败。已提交错误通过 `MutationCommitted() bool` 标记，保留原始错误链；恢复 checkpoint 事件持久化失败时，当前会话已经切换，journal 仍待重试。Runtime 的新建、恢复、删除均在成功或已提交失败时发布 `session_changed`，并向调用方返回实际错误。返回会话快照或元数据的边界不得因为同时存在错误而丢弃该值。

运行耗时由 Agent 的执行生命周期持有：运行期间增长，最终结果整理完成、发布运行摘要前固定；循环终止标记和取消请求本身不结束计时。会话恢复、清空或 Agent 重建时清除上一轮耗时，读取指标与计时更新使用同一把锁。

`config.Save` 在内部克隆调用方配置再校验，成功和失败均不修改调用方数据。保存将完整内容写入同目录的私有临时文件，完成文件同步和关闭后以 rename 替换目标；rename 是提交点，失败清理临时文件。已有符号链接保留，更新其目标文件。该契约保证原子可见性，不承诺断电后的目录项持久性。

持久化模型切换和推理深度设置先修改独立配置副本，保存成功后再替换运行配置并重建 Agent；保存失败不改变当前 Agent。临时会话设置沿用同一应用路径，跳过磁盘保存。

配置重载重建工具、扩展、策略和 Agent，不重建会话。已加载技能标记、审计事实和 completion 总量随当前会话保留，用量恢复统一使用替换接口；历史读取记录不会恢复为新运行的编辑授权。

## 文本事件契约

`assistant_delta` 是可丢弃的推测文本，`assistant_reset` 撤回当前响应的推测部分，已经确认的工具前言不受影响。
`assistant_message` 是已确认的响应，终态 `RunResult.Output` 是最终输出。自定义 Runner 应返回最终输出，而不是仅调用 `host.Text` 后返回空字符串。

支持撤回的适配器可显示 delta；ACP 的文本块仅追加，因此只发送已确认消息和未重复的成功终态输出。推理和工具事件仍实时发送，不缓存无限增长的推测文本。取消或失败不提交未确认文本。
ACP 使用可靠订阅；事件队列溢出应报流失败，不能静默丢失文本后继续报告成功。

## 工具工作区

Executor 通过 context 注入 `workspace.Manager`。Bot 在组装时固定主目录；相对读写路径和额外相对 roots 都锚定该主目录。Shell 使用会话工作目录，Index 与 LSP 固定到工具实例首次使用的项目根；跨项目复用实例会明确报错。任何适配器不得通过进程 `chdir` 切换项目。
额外 roots 表示读写授权，不表示执行目录。独立调用工具、未注入 Manager 时仍以启动时 cwd 作为兼容默认值。

## 工具结果与请求预算

工具普通执行和提权重试返回 `core.ToolOutput`，文本预览与原始文件引用分离；文件引用通过结构化字段传递，不解析展示提示。执行器只授予当前 actor 输出目录内普通文件的只读访问，二次限流复用原始文件。

待交付工具消息保存独立文件元数据和有界的 `DeferredContent`。恢复交付时校验文件归属，再恢复原预览，避免从原始文件重建时丢失错误说明。会话输出存储由 Bot 的会话组装持有，执行器和上下文使用同一个实例。

`PrepareRequest` 是主 Agent、子 Agent 和最终合成的统一请求准入点：自动压缩阈值与最终请求预算共用至多一次压缩机会，压缩后重建并检查请求。摘要按实际格式化消息、输出上限及安全余量校验，摘要自身不会触发递归压缩。

委托任务的完成、部分完成和失败状态必须保留；失败和取消向上返回原始错误链，已有证据随错误交付。部分结果即使没有错误也明确标注尚未完成。

## 协议载荷

新增 Runtime 事件时，必须在 `runtime/internal/core/event_payload.go` 登记其单一载荷 codec，并在事件契约场景中覆盖其外部可观察语义。记录回放、headless 与 ACP 对已知事件使用相同的载荷类型校验；未知事件保留前向兼容。适配器不应复制 Runtime 状态转换逻辑。

## 验证

- 核心边界：CI 独立运行 `go test ./internal/architecture`；全仓测试也会执行它。
- 协议与生命周期：`go test ./acp ./headless ./runtime/... ./bot/agent/... ./bot/core`。
- 全仓：`go test ./...`；并发相关变更同时运行 Bot、Runtime 与事件适配器的 `go test -race`。
- HTTP 观测契约用真实 Runtime 和轻量 Runner 构造状态，不在契约测试中复制过滤、聚合和指标计算。
