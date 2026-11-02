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

## 结构化进程观察

Shell 与 process 的 ToolOutput.Process 携带 ProcessObservation 值，executor 传入 ToolCallResult，并在 TextOutput 中保留。State 是既有进程状态 running/exited/killed/timeout/unknown；只有正常退出才设置 ExitCodeKnown，零值退出码本身不能证明成功。未知 Wait 错误不补造退出码 1。非零退出与工具层 error 独立，nil error 只表示工具调用返回，不表示命令成功。

状态、展示信息与输出在原 shellTask 锁内一起采样。OutputComplete 仅在进程已结束、输出管道读完、没有读取异常且当前样本含从起点开始的全部捕获字节时为 true；运行中、环形日志覆盖、预览裁剪和 watch 增量不能宣称完整。该标记针对格式化前的捕获输出，不保证原始字节经 ANSI/换行处理后仍相同。executor 再裁剪或保存失败会清除完整标记，已观察的状态和退出码不改写。process list 不生成单个进程观察，未启动或未提供观察的工具保持空值。

进程观察为 executor 的执行凭证提供事实，不能从展示文本或 nil error 推导命令成功。模型展示仍沿用原文本，观察不授予权限。执行凭证保存边界见下节。

## 工具执行凭证

executor 在每次实际进入工具函数前生成 UUID，记录 run/task/attempt/call 关联、有效工具名、公开参数 SHA-256、起止 Unix 纳秒、边界返回摘要、错误摘要、进程观察和原始 artifact 元数据。权限重试是第二次物理调用，使用独立 ID；工具进入后返回 PermissionError 记 blocked，仅表示权限请求返回，不证明此前没有副作用。调用前拒绝、治理阻止、未分派的队列取消和内部 submit_result 不补造物理执行凭证。

状态为 success/error/blocked/cancelled/unknown。正常进程退出按真实退出码分类，运行中或未知观察保持 unknown，信号终止/硬超时为 cancelled，工具 panic 为 unknown。没有进程观察的自定义工具仅按实际 error/nil-error 表达工具返回状态，success 不证明业务完成或测试通过。raw_complete 只沿用显式进程输出完整性且拒绝 incomplete artifact；缺生产者事实时为 false。摘要不签名、不授予访问权限，也不读取 artifact 正文校验文件版本。参数序列化不可用时摘要缺省，不散列虚假的空参数；原始参数和错误不复制进凭证。

ToolCallResult.Evidence 的 schema=1 聚合当前调用至多两次物理调用，并记录 executor 完成归一化、裁剪后的返回摘要及 normalized/bounded/preservation_failed。原始工具返回的摘要与 executor 返回摘要分开；文本先按原始字节散列，避免 JSON 将不同无效 UTF-8 字节归一成同一值，也避免为大正文分配 JSON 副本。归一化和保存失败不改写物理调用的真实结果，不自动重放工具。

主、子代理沿既有工具结果接纳链路传入 Context。Context 在结果接纳及 Snapshot/TranscriptTail 导出、Restore 输入边界克隆执行列表，防止调用方通过快照修改权威凭证；只在该工具的权威 transcript 记录附加 ToolEvidence，并记录初次接纳时的可见正文摘要及 ready/budgeted/deferred；该摘要不是 provider 接收回执，也不随后续 deferred 恢复改写。活动消息和 session.json 不保存凭证正文，恢复时保留 transcript 元数据并从活动历史移除；provider wire 与 prefix 指纹也排除该字段。继续使用现有 transcript 水位和保存事务，没有独立事件仓库或凭证索引。旧记录缺字段不补造；旧版本重写可能丢失新附加字段。

凭证已生成、保存，并开放当前执行者及父会话指定子 attempt 的 evidence 查询；模型自动有界引用及父子 evidence IDs 交接见下节。主、子代理工具完成回调携带 executor 交付时的 ToolEvidence 快照，回调前克隆执行列表，Runtime 事件总线在历史保存、观察者/订阅者交付和回放出口统一分离凭证切片，防止观察者污染其他消费者；凭证字段计入原有事件历史字节预算；只有 tool_completed 的 ToolPayload 可带可选 evidence。复用既有 ToolPayload codec 与 recording 序列化/回放，不新增事件或凭证仓库。该快照早于 Context 接纳，不含后续 visible_hash/admission，不能证明 transcript 已落盘，也不由 Runtime 的终端输出归一化重算哈希。headless 的工具结果 user 帧可带 nekocode_evidence；ACP 的 tool_call_update 通过 _meta["nekocode/evidence"] 携带。无凭证时字段省略，旧事件和旧客户端保持原结果结构；工具阻止、预览、收尾补齐不补造凭证。事件是派生观察，不能反向覆盖 transcript 或作为恢复重放工具的依据。历史保存失败仍由原有调用方返回错误，不把失败解释为工具未执行或自动重试的许可；历史及录制失败的结构化表达见下节。

## 执行历史保存失败

主会话 saveSession 和子代理 SaveContext 在保存失败时通过 protocol.MarkEvidenceIncomplete 包装原始错误；errors.Is/As 仍可读取原因，取消错误可以与保存失败并存。标记只表示历史可能不完整，不推断工具是否执行或事务是否完全未提交，也不允许自动重放工具。主会话保留原输出及 pending transcript，不推进保存确认水位；子代理停止正常循环，最终检查点可以重试保存既有事实，不重复执行工具。已有的未知结果恢复继续显式提示 unknown outcome。

执行器的 ToolCallResult.Cause 仅在进程内保留原始调用错误，不进入 JSON、模型消息或凭证正文。主 Agent 在现有 runState 中最多保留首个带标记的工具错误，允许模型完成父任务回答，随后与运行结果错误合并；因此子历史保存失败不会在 task 展示字符串边界丢失，也不会把普通工具错误升级为整轮失败。新 Run 清空该值。

Runtime 从 runner 的结构化错误派生终态 RunResult.evidence_incomplete，并沿同一事件更新 RunSnapshot、codec 和 recording 回放；不从错误文字推断该标记。取消和关闭先等待 runner 收尾，现有 runExecution 仅保存该次运行的布尔收尾事实，防止取消状态遮蔽随后发生的保存失败。标记不改变已有失败/取消状态，不增加 stop enum 或状态仓库。headless result 可带 nekocode_evidence_incomplete；ACP 失败响应的 error.data、取消/正常响应的 _meta 可带 "nekocode/evidence_incomplete"。false 时字段省略，旧调用者保持原结构。

录制错误由 EventRecorder 的现有 runRecording 保留该 run 首个失败，EndRun 释放；全局首错仅供 Close 汇总，不污染下一轮。RecordError 在同一锁内把已知失败加入即将写入的终态，RecordRunEvent 继续复用该写入路径，并在终态关闭日志和文件锁句柄。编码、写入、指标 flush、绑定和句柄关闭失败均计入；载荷无法编码时不再以 marshal_error 替代正文并假装成功。Runtime.Close 通过相同的结构化标记返回录制原始错误，注册阶段失败则直接返回给 StartRun，不启动 runner。

EventBus 的单一 recording sink 替换原录制观察者：分配原有事件身份后，先尝试录制和终态关闭，再把补充 evidence_incomplete 的同一个事件交给 history、RunStore、观察者及订阅者。sink 不重入 Publish、订阅或 Close；现有观察者等待机制覆盖这段 IO。取消和关闭也先绑定该 run，再通过同一终态入口发布。不增加事件、重传队列或长期错误索引；已有 run_done、输出和工具事实保留，录制失败由独立标记表达，不自动重放工具。

最后一帧写入或其后关闭失败时，实时终态仍带标记；磁盘旧事件可能不包含该次失败，重启后无法保证恢复出丢失的标记。没有标记不等于可靠落盘或 fsync 回执。磁盘已写入的事实不会因录制失败反向覆盖 transcript。

## 执行凭证查询

只读 evidence 复用 context_refs 的查询解析器、refbridge.Source 和执行者 reader 绑定，允许 Plan Mode。Kind 由工具实例固定，模型不能指定其他 session 或路径。主入口默认返回主会话凭证，可同时提供 task_id/attempt_id 查询当前父会话下指定的已保存子执行；子入口核对当前 task/attempt/OutputStore，只返回自身任务的凭证（含自身历史 attempt），不查询父会话或兄弟任务。完整执行身份缺失的子运行不能查询；内置 coder/explore 包含 evidence，自定义 Profile 白名单分别控制 evidence 与 context_refs。仅开放 evidence 不会获得 context_refs 自动投影。

list 的 limit 为 1–20 条原始历史记录，并最多返回 20 条凭证摘要；它复用现有索引的单记录定位，倒序合并已确认持久化水位和 pending tail，不增加凭证索引或改动技能/产物索引资格。64 KiB 为来源软预算，首条不可拆分，边界处可能先解码一条候选记录再延后返回；scanned 计入该候选，但 before 不跳过它。并非全历史搜索或常数冷恢复 IO。稀疏页可以没有 evidence 条目但 has_more=true，必须沿 before 原始排他游标继续。返回上限不拆开同一条来源中的两次物理调用，旧页不会跳过被延后的来源。

返回 evidence 数组中的 ID 是 `evidence.<原始序号>.<scope及凭证元数据SHA-256>`，receipt_id 为宿主生成的物理 UUID。inspect 只定位该序号的一条记录并核对完整 ID，匹配时 status=recorded，否则 missing；recorded 仅表示记录关联，不表示 verified、测试通过或任务完成。list 不返回正文；inspect 的 detail 仅含目标物理调用和交付摘要，剔除 artifact 路径，has_artifact 只表示来源声称携带产物。查询不访问文件系统核实产物，不授予文件读取或工具重放许可；底层错误转为固定错误，取消保持原有错误语义。

父代理选择子任务时，从权威 session.json 验证 ID、parent、格式、attempt 起止序号及已提交水位；固定该区间，再核对每条 receipt 的 task 和精确 attempt。不读取未提交尾部，也不因子代理正在追加而修复历史或重建索引。复用只读历史定位，索引缺失或落后时流式扫描，可取消；不保证冷查询常数 IO。子入口拒绝所有 task_id/attempt_id 选择器。主模型、最终合成及允许 evidence 的子模型请求自动接收 `evidence_references`，最多 16 条，仅含 ID、原始序号、工具、状态及是否有产物。每次采样复用 evidence list 的最近 20 条原始记录和 64 KiB 软边界，不承诺稀疏旧凭证自动留存；较旧凭证可继续 list 分页或按已知 ID inspect。完整凭证、路径和输出不进入投影；投影不写回活动消息或 transcript。存在凭证页时，技能/产物先占最多 2 KiB，凭证使用 4 KiB 总额的余量；无凭证且没有更早页时原视图仍可用满 4 KiB，与任务视图合计保持 8 KiB。字节预算和 16 条上限均按原始记录整组裁剪，不拆开同一调用的普通执行与权限重试；游标锚定最旧保留记录，空页从 0 重查。扫描计数和移动原始页边界不进入缓存前缀。不可用只投影 unavailable，不暴露底层错误，不重放工具。查询本身作为工具调用也会产生正常凭证，返回前的来源水位不包含该次查询尚未接纳的结果。

## 子代理凭证交接

内部 submit_result 的 evidence_ids 为可选数组，最多 16 个唯一的规范 evidence 引用；缺省仍兼容旧子代理，不改变原有必填交接字段。该字段只保留引用，不复制凭证正文。已保存的提交调用及交接文本保留这些 IDs，不另建交接状态仓库。

任务启动时固定 parent session，恢复、worktree、子执行身份和返回核对都沿用该值。Engine 完成最终 checkpoint 返回后，宿主用父执行者的既有 reader 对提交的 IDs 逐项查询指定 task/attempt，并向父工具结果追加带 task_id/attempt_id 的有界关联 JSON。resolved 只证明引用与记录匹配，并同时保留原 execution_status；执行 error 仍可被 resolved，绝不等于测试通过或任务完成。unknown 表示该范围找不到匹配记录，wrong_scope 表示引用序号或实际 receipt 不属于指定 attempt，error 表示当前权限、取消或存储不可用。格式合法但不匹配的摘要不补造记录。

关联投影不反向修改子任务结果或执行凭证，不改变任务终态，也不因查询或保存失败重放工具。未提供 evidence_ids 时不额外查询；父会话切换后拒绝沿旧任务关联到新会话。产物路径仍不返回，原有权限和文件读取边界不变。

## 协议载荷

新增 Runtime 事件时，必须在 `runtime/internal/core/event_payload.go` 登记其单一载荷 codec，并在事件契约场景中覆盖其外部可观察语义。记录回放、headless 与 ACP 对已知事件使用相同的载荷类型校验；未知事件保留前向兼容。适配器不应复制 Runtime 状态转换逻辑。

## 验证

- 核心边界：CI 独立运行 `go test ./internal/architecture`；全仓测试也会执行它。
- 协议与生命周期：`go test ./acp ./headless ./runtime/... ./bot/agent/... ./bot/core`。
- 全仓：`go test ./...`；并发相关变更同时运行 Bot、Runtime 与事件适配器的 `go test -race`。
- HTTP 观测契约用真实 Runtime 和轻量 Runner 构造状态，不在契约测试中复制过滤、聚合和指标计算。

## 文件版本与修改前提

模型管理的 executor 为每个主/子执行者持有独立的 `ExecutionState.Reads`。专用文本 read 从同一次原始字节读取生成 SHA-256 和展示文本；FileStateCache 仅缓存给定原始字节版本的解析文本，Lines(path, version)/Put(path, lines, version) 显式要求该版本，Go 嵌入方需同步签名；不再维护已读行范围或 mtime/size 命中回退。mtime 只用于父子缓存合并排序，展示 TAG 不作为修改凭证。每份读取凭证只保存路径、版本和预览摘要，最多 128 条，超限淘汰最早记录；不复制到父子缓存或快照合并。

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

Skill 诊断由 Context 在最终请求投影中派生，不读取 Skill 注册表或将 LoadedSkills 当作仍持有全文的证据。成功 skill 工具结果通过已有 SkillName 标记识别；手动选择、`$skill` 命令加载和子代理的请求级工作流使用内部 `skill-workflow` 来源。普通用户或工具正文中的 `<skill_content>` 标签不能生成这种来源。失败或 deferred 结果不计入；新版本接纳记录为 incomplete 时也不计入，完整预览恢复后才参与摘要。工作流保持 user 角色、来源不进入 provider wire，命令后的真实用户参数仍单独计为用户回合。

现有 prefix 诊断增加 versioned `skills_hash` 与 `skill_messages`，覆盖当前请求里有明确来源的、按顺序出现的实际技能内容；新增、移除或修改会标记 `changed_parts=skills`。数量表示消息数，不是去重后的技能数。摘要不进入提示词，不回写 transcript，也不维护第二份技能状态。旧会话和缺字段会话仍可恢复；首次恢复请求只重新派生当前摘要，不根据缺失的旧技能摘要虚报变化。旧版本未标来源的手工技能消息仍作为历史保留，但不能从其文本猜测成已识别技能。

技能目录的描述变更仍由系统 prefix hash 观察；尚未交付的技能正文不作为当前模型行为证据。压缩移除完整技能消息后，当前请求的技能摘要相应消失；摘要中的自然语言提及不等于模型仍持有技能全文。该诊断不保证技能语义保留、权限有效或外部辅助文件未变化。

## 权限与目录观察

主模型、合成、子代理请求在准备请求前，从各自 executor 采样 `AccessObservation`，经 context 传入现有 calllog 的 `access`；请求触发的压缩沿用该样本，主会话尾部压缩重新采样。子执行者覆盖父样本，权限模式沿实际 modeSource 读取，不能将起始模式视为整轮固定权限。

permission 所有者分别生成有序规则声明和当前项目持久授权的 `v1:` SHA-256；路径解释锚点参与规则摘要，时间戳和授权理由不参与。授权摘要每次只读加载当前项目权限文件，因此其他 Store 实例的持久授权更新也可见；读取失败仅标记 `store_unavailable`，不改变执行权限。一次性审批不伪装为持久授权。workspace 所有者在锁内取得主目录与当前会话有效目录的独立快照，在锁外排序和摘要；切换会话后临时目录按实际范围重新观察。

日志只保存 schema、模式、plan 状态、摘要、存储不可用及外部判断器是否配置，不输出规则正文、目录清单、授权理由或凭据。观察不进入提示词，不写回会话，不执行 matcher 或判断器，不新增配置缓存、版本表或管理器。各领域分别采样，不构成原子授权事务；规则摘要不覆盖 matcher 实现，判断器存在性不代表其行为指纹，也不保证整个执行可复现。

## 任务连续性引用

bot/core 通过已有 TaskLister 取得当前父会话的任务元数据，Agent 将值传入 `ModelRequest.TaskReferences`。Context 不查询 Runtime、session 或任务目录；主模型与最终合成都在 Archive 后、活动 history 前接收一条 user-role JSON `context_data`，kind 为 `task_references`。它携带父 session、任务 ID、recorded、最新 attempt ID/序号、状态、简短标题和 Profile，以及总数、omitted count 和 `task_list` 查询入口。任务 ID 用于查询/恢复任务，attempt ID 仅标识一次执行，引用不代表授权或允许重放。

任务与 attempt 的合并继续使用 Runtime.Subagents 原有权威投影，不在 Bot、Agent 或 Context 重新实现终态规则。宿主查询显式固定父 session，并核对返回 scope 和当前会话；模型的 task_list 参数不能指定其他 session。读取失败或 scope 不一致只产生 unavailable，不向模型透传存储错误或其他会话条目。引用是采样数据；实际恢复和工具读取仍检查当前状态与权限。

投影最多 16 条且完整 JSON 不超过 4 KiB，标题和 Profile 先取有限预览，超限时移除条目并更新 omitted，不截短任务或 attempt ID。默认优先非 completed 任务，同组按最近更新时间排序，再以 ID 保持稳定；普通 task_list 的创建时间顺序不变。查询只读取元数据，不读取子任务 transcript 或完整模型上下文。首次及任务操作后的查询仍会遍历全部任务元数据，Limit 并不承诺底层分页 IO。

为避免每次推理扫描磁盘，现有 runState 仅复用本轮最多 16 条请求引用；新 Run 自动清空，实际 task/task_list 工具批结束后失效，下次请求重新采样，不可用结果不缓存。普通工具和最终合成复用本轮视图；同轮外部进程修改在显式 task_list 或下一轮刷新。不新增任务索引、全局缓存或持久化状态。

引用只进入请求副本和相同请求的预算、prefix 计算，不写入活动消息、transcript 或 session.json，不送入历史摘要。每次 PrepareRequest 重建都带入同一份引用，因此连续压缩、恢复和模型切换后仍可重新注入。旧会话无需迁移；技能和产物的长期发现与按 ID 查询见下文。

## 产物引用与查询

主代理的 `ContextReferences(ctx, sessionID, kind)` 单一回调由 bot/core 组装，宿主传入空 kind 查询技能/产物、evidence kind 查询凭证，不再分别配置 EvidenceReferences；Go 嵌入方需同步此回调签名。复用当前会话历史索引的引用链接，Context 只接收 refbridge.Page 值。主模型、合成请求和开放 context_refs 的子模型请求在 Archive、任务引用之后，活动 history 之前投影 user-role JSON `context_references`；不写回活动消息、transcript 或 session.json，不复制工具正文，不新增独立引用存储。技能最多 8 条、产物最多 16 条，与凭证引用共享不超过 4 KiB 的完整 JSON 预算，与任务引用合计最多 8 KiB。只投影最新的有界引用页；更早引用按原始序号分页读取，不生成全会话产物目录。

来源每次最多读取 20 条含引用的历史记录，复用 transcript.idx 跳过无关记录，使用 64 KiB 软预算；首条不可拆分，已有工具记录最高 4 MiB，底层记录解析上限为 16 MiB。已有索引每项从 32 字节增至 40 字节，增加引用链接和索引项校验，64 字节头不变；权威 transcript 和 session 格式不变。索引缺失或旧版格式时流式重建一次，可取消；查询不修复或改写 transcript。有效索引按引用跳转，新增历史仍需解析新增记录，尚未保存的尾部仍向后扫描，不能宣称冷恢复绝对恒定 IO。索引可删除重建，不缓存正文或增加全局状态。

自动投影不携带扫描计数、移动窗口边界或文件路径，较旧查询的游标锚定最早保留的引用；引用集合不变时，不因普通消息推进而改变缓存前缀。超限移除条目后游标不会跳过被省略项。展示分页附带的 assistant header 不计入原始序号。

`context_refs` 是当前执行者的只读工具，允许 Plan Mode；list 每页读取 1–20 条含引用的来源记录，按新到旧返回产物或技能引用。`before` 是原始历史的排他游标，不是引用偏移，has_more 表示还有更早引用。模型不能指定会话或文件路径。ID 使用 `artifact.<原始序号>.<SHA-256>`，绑定执行者 scope、原始序号及结构化 ToolOutputArtifact；同一来源在压缩、保存和恢复后 ID 不变，fork/会话变化不会继承旧 ID 的 scope。

inspect 按 ID 定位一条原始记录，核对 ID 与来源元数据，再验证执行者的 OutputStore、当前 workspace 读权限和文件归属，只有全部通过才返回文件位置；正文仍用现有 read 工具重取，读取时再次检查权限，不从引用恢复写前读许可。工具入口必须绑定执行者。主入口用 TaskID 和主 OutputStore 身份拒绝子执行者，包括未命名的旧子执行器；无执行者 context 的内部主模型投影只读取元数据。子执行每次 Run 在 context 注入自己的 reader，显式遮蔽继承的父 reader，不改共享 registry；查询核对当前 TaskID、AttemptID 及 actor OutputStore 指针。子 scope 为 `<parent>/subagents/<task>`，跨恢复 attempt 稳定，换父会话或任务即改变；Page.session_id 在子查询中返回此 scope。持久化来源绑定已打开的子会话，仅合并当前已确认水位与内存尾部，不使用活动消息推断历史引用。未提供完整执行身份的嵌入运行不开放查询。内置 coder/explore 包含 context_refs 和 evidence；自定义 Profile 仍需显式允许，未允许时不投影入口，已有 Profile 不被隐式扩权。恢复旧引用不自动增加文件访问权限，正文仍需当前 read/skill 工具许可。状态 missing 表示来源/已授权文件缺失，changed 表示来源元数据与 ID 不匹配，unavailable 表示当前权限、归属或存储不可用；存储错误不向模型透传原文，不修改已有历史。

ID 的摘要不是保存文件内容的版本指纹；available 只说明采样时的位置可读取，不保证文件从未被外部修改，也不证明工具成功、任务完成或允许重放。当前支持主会话或当前子任务保存的工具输出，不含其他执行者目录查询、任意生成文件或完整历史产物目录；技能引用使用同一工具，契约见下节。

## 技能接纳与引用

`Message.SkillDelivery` 是 schema=1 的可选本地值，记录技能名称、来源、接纳状态、正文 SHA-256 和原始 transcript 序号；不会进入 provider wire，也不参与 provider history 指纹。它表示宿主接纳事实，不是 provider 收到内容、模型理解内容或执行技能的凭据。ready 表示完整选定文本已就绪，deferred 表示等待空间，failed 表示未成功接纳，incomplete 表示空或带裁剪产物的预览不能证明完整性。历史无该字段时保持未知，不从 SkillName、LoadedSkills 或正文标签补造记录；旧字段仍可兼容既有可见内容诊断，但不生成新的历史技能引用。

普通 skill 工具结果在 Context 接纳边界记录 tool 来源；失败同样保留请求的技能名称，但不登记已加载或生成引用。显式选择和 `$skill` 命令由所有者传入名称，来源为 selected/command；旧的无名称选择接口仍可用，不猜测其名称。委派由 bot/core 提供与正文同序的 resolved SkillNames，子代理克隆两组值并校验长度。首次 PrepareRequest 成功后，每 attempt 为每份选定工作流记录一次 delegated 元数据，超窗未准入时不记录；不依赖 Profile 名单或标签来猜选定技能。

完整 deferred 预览恢复前，在原有全批预检中核对正文摘要；不匹配则不改变消息、记账或历史。恢复成功只将活动消息改为 ready，并在现有 transcript 追加一条引用原始序号的 metadata-only runtime-event；旧 deferred 记录不改写，不复制正文。裁剪预览恢复后仍是 incomplete，不能登记已加载或形成 ready 引用。事件不加入活动模型上下文；重启时即便它来自未提交历史尾部，Context 恢复也将其留在 transcript、从活动上下文剔除。没有额外事件仓库、manager 或技能状态索引。

`context_refs list` 在同一分页边界中返回带 kind 的产物或技能引用；只有 schema 与所有者来源匹配的 ready 记录生成 `skill.<原始序号>.<scope及元数据SHA-256>`。近期自动投影使用统一 `context_references`，技能和产物共享现有 4 KiB 配额，不为技能额外扩容；达到类型上限或字节预算时保留连续的较新来源前缀与可查询的旧游标。压缩或保存不改变原始序号和引用 ID，较旧引用可继续分页或按已知 ID 查询。

技能 inspect 检查原始记录、当前执行者 scope 和当前 extension 注册版本的格式化正文摘要：缺技能为 missing，摘要不同为 changed，相同为 available，不能访问当前注册表或 actor 时为 unavailable。tool 来源按执行器相同的 ANSI/换行规范化后比较，selected/command/delegated 比较其原始选定文本。available 只说明当前注册内容匹配，不代表外部文件尚未修改、辅助文件相同、模型仍持有全文，或模型被允许重新加载；实际 skill/read 工具仍应用当前调用限制和权限。技能正文与辅助文件不复制进引用。
