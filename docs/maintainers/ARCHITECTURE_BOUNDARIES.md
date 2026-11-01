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
- `protocol.ExecutionIdentity` 只通过 context 传递观测身份，不参与权限判断。Runtime 入口绑定自己的 run ID；直接 Bot/Agent 入口缺少身份时创建独立 run ID。任务装配层通过 `subagent.RunConfig` 显式提供 task/attempt 身份，引擎只继承父 run ID，未提供的持久化 attempt 不得猜测或继承。
- 模型调用日志的通用字段由 `calllog.NewRecord`/`SetOutcome` 统一组装；主模型、合成与压缩保持各自执行/用量生命周期。共享 provider 不保存 run/attempt 身份；每次物理调用有独立 call ID，steer 替换取消上下文不改变本次 run 身份，尾部 housekeeping 压缩保留身份但不继承取消。
- Context 不依赖 Agent、Policy 或 Session；文件工具不依赖 Policy。跨层数据由现有组合根/执行边界传入，不为观测功能增加全局注册表或第二份权威状态。这些方向由架构依赖测试覆盖。
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

Memory/Archive 只保存原文，在 Context 的请求边界统一投影为带内部来源的 JSON user 数据，静态 system 层只解释其历史数据语义。投影不回写 transcript，不成为新用户回合或操作授权；回合识别与报告共用 `Message.IsConversationUser`，不解析正文标记。缓存指纹取自同一次实际请求投影，固定规则与编码开销计入预算。OpenAI/Anthropic 仅编码消息，不能将历史正文提升进 system。

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

## 文件版本与修改前提

模型管理的 executor 为每个主/子执行者持有独立的 `ExecutionState.Reads`。专用文本 read 从同一次原始字节读取生成 SHA-256 和展示文本；FileStateCache 只有原始版本相同时才复用文本，mtime/size 与展示 TAG 不作为修改凭证。每份读取凭证只保存路径、版本和预览摘要，最多 128 条，超限淘汰最早记录；不复制到父子缓存或快照合并。

读取成功只生成待交付的本地 `ReadID`。Agent 在每次实际模型请求前，通过同一适配器核对最终 tool 消息及完整预览摘要，重新确定当前可用凭证。失败、截断或 deferred 占位不能授权；延迟预览完整交付后可生效。局部读取证明片段来自当前版本，不声称读过全文。媒体与 PDF 不生成文本修改凭证。

`ReadID` 不进入 provider JSON、transcript 或持久会话。新 run 清空凭证；Restore 和成功压缩清除活跃消息中的标记，下次请求不再授权旧读取。压缩失败保留原状态。write/edit 成功后清除当前执行者对目标的凭证；write 失败也清除，因为底层写入可能已部分发生。再次修改须重新读取；oldString 锚点不能替代当前版本读取。Policy 的先读规则与工具的版本核对分别负责顺序约束和最终修改前提。

专用文本 read、write、edit 共用 execution 的规范路径锁，取代 edit 的私有锁表。等待可取消，引用归零回收，审批在锁外；锁内重新校验原请求路径及目标，随后核对原始版本并修改。现有文件缺凭证或版本冲突时明确要求重读；新文件在锁内确认不存在后可创建。文件工具不依赖 Policy、Agent 或 Context，Context 只搬运本地标记。

该保证覆盖同一进程中的合作文件工具。Shell、外部编辑器、其他进程、硬链接别名不受该锁约束；版本检查可发现检查前发生的变化，不提供跨进程 CAS。每次文本读取均读取磁盘原始字节以核验版本，缓存主要节省文本解析，增加的读取成本属于明确取舍。低层直接调用工具且不提供 ExecutionState 时保留原有 API；模型路径必须使用 executor 或显式提供独立 ExecutionState，不能用全局读取凭证补位。

## 摘要压缩事务

压缩继续由 Context 的 replacement compactor 负责；不新增摘要仓库、后台任务或会话格式。当前用户回合及工具配对留在 recent history，旧消息与旧 Archive 才参与处理。内部 runtime 快照和短期 hints 不进入摘要；显式配置的相关性 pruner 仍可先将旧结果替换为占位。

摘要输入是 user 消息内的 JSON 数据记录，保留完整正文、工具名/参数、call ID、错误标记及产物引用，不再按 500/800 rune 截断，也不会遗漏没有正文的工具调用。图片仅记录数量，私有推理与图片正文不送给文本摘要模型；摘要不能据此推断图片内容。

单次请求放得下时维持一次调用。超出压缩模型窗口时，旧 Archive 按时间顺序先于消息参与装箱；超大记录序列化后按 rune 边界拆分，片段携带源内容 SHA-256、序号与结束标记，不引用会随压缩移动的活动消息下标。后批只携带前批摘要作为历史数据。预检为每批预留输出上限、同等规模的中间摘要和安全余量，超过 8 批直接失败，不发送部分源输入。未知压缩模型窗口时使用 64K 保守默认值；预算仍是启发式估算，不等于模型分词器的精确计数。

一次压缩最多 8 次实际模型流调用，空响应重试也消耗该额度。每次物理调用沿用原有用量记录与 calllog，不对批次或整次压缩再次记账。压缩开始/结束事件仍各一次，中间 delta 可包含多个批次的输出；失败事件中的流文本只是未提交结果。

中间摘要只保存在调用栈内。所有批次必须返回完整、非空的 `<summary>...</summary>`，且不超过预留输出预算；不再接受无标签正文、未闭合标签或以占位摘要删除旧历史。全部成功后，压缩在锁内检查 revision，并在替换历史前验证候选上下文及输出余量：由 PrepareRequest 触发时沿用实际请求的输出上限，手动及 housekeeping 压缩使用默认余量。失败、取消、并发输入、最终超窗均不提交 Archive、消息或压缩计数；已发生的模型调用费用仍按事实记录。before-compaction 备份保持原有前置边界，after-compaction 只在提交后触发。请求专有的工具 schema、临时消息和实际输出设置仍由 PrepareRequest 在重建后做最终准入检查。

## Policy 配置诊断

`Policy.Configuration()` 从当前注册的有序 Hook 声明生成 schema=1 的规范 JSON SHA-256，只包含名称、调用点、声明的行为版本、Guard、有效超时（纳秒）、Once 与 SessionScoped。执行顺序保留，默认超时按实际生效值归一化；Hook 私有状态、会话、调用计数与回调实现不参与摘要。日志只输出 schema、hash、进程内注册 revision 与 incomplete，不输出声明正文。

Hook 所有者通过 `BehaviorVersion` 声明行为版本；内置 Hook 明确声明版本，自定义或插件 Hook 未声明时标记 incomplete。非空版本只代表所有者的声明，不保证代码或外部依赖可复现，也不能推断未知回调的完整行为。相同声明跨进程 hash 一致；revision 仅在本进程内标识注册替换和删除，不能作为跨重启版本。状态重置不改变配置摘要。

主模型与合成请求在 PreModel 后快照当前 Policy，子代理快照自己的 guard，覆盖父上下文中可能继承的策略诊断；父策略的 audit-only 回调不冒充子代理授权策略。请求准备中发生的压缩继承同一快照，Bot 尾部压缩重新获取当前策略快照。`calllog.NewRecord` 通过 context 接收值对象；Context 不依赖 Policy，不执行策略扫描。该诊断只反映采样点的注册声明，不记录整轮固定的权限模式，也不授予任何访问权限。

## Profile 与 Skill 配置诊断

子代理在 Run 的 Profile 校验、克隆后，从 Profile 所有者获取 `ConfigurationHash()`，以 context 值传入现有 calllog 的 `profile_hash`。摘要为 `v1:` 加完整 SHA-256，包含名称、声明模型、隔离模式、工具集合、按声明顺序排列的技能和实际默认步数。工具集合排序去重，nil 与空列表等价；读取摘要不修改 Profile。它描述 Profile 的声明，不宣称包含所有运行配置。实际模型、工具 schema 与系统提示仍分别由 provider 元数据和既有 prefix 诊断覆盖，不重复保存提示词或配置正文；子代理覆盖父上下文中的 Profile 摘要。主代理没有命名 Profile，主模型、最终合成及主会话尾部压缩显式清除继承的 Profile 摘要，避免将上游子代理配置误记为自身配置。

Skill 诊断由 Context 在最终请求投影中派生，不读取 Skill 注册表或将 LoadedSkills 当作仍持有全文的证据。成功 skill 工具结果通过已有 SkillName 标记识别；手动选择、`$skill` 命令加载和子代理的请求级工作流使用内部 `skill-workflow` 来源。普通用户或工具正文中的 `<skill_content>` 标签不能生成这种来源。失败或 deferred 结果不计入，恢复完整交付后才参与摘要。工作流保持 user 角色、来源不进入 provider wire，命令后的真实用户参数仍单独计为用户回合。

现有 prefix 诊断增加 versioned `skills_hash` 与 `skill_messages`，覆盖当前请求里有明确来源的、按顺序出现的实际技能内容；新增、移除或修改会标记 `changed_parts=skills`。数量表示消息数，不是去重后的技能数。摘要不进入提示词，不回写 transcript，也不维护第二份技能状态。旧会话和缺字段会话仍可恢复；首次恢复请求只重新派生当前摘要，不根据缺失的旧技能摘要虚报变化。旧版本未标来源的手工技能消息仍作为历史保留，但不能从其文本猜测成已识别技能。

技能目录的描述变更仍由系统 prefix hash 观察；尚未交付的技能正文不作为当前模型行为证据。压缩移除完整技能消息后，当前请求的技能摘要相应消失；摘要中的自然语言提及不等于模型仍持有技能全文。该诊断不保证技能语义保留、权限有效或外部辅助文件未变化。
