# Harness 改进计划

状态：进行中。日期：2026-11-01。仅保留未完成的执行证据设计；已实现契约统一维护在架构现状文档。

依据：[DeerFlow 调研](DEERFLOW_RESEARCH.md)、[架构边界](../maintainers/ARCHITECTURE_BOUNDARIES.md)、[通用运行预算](RUN_BUDGET.md)。实现基线为 NekoCode `b40909b`。

## 1. 推荐方案

沿现有模块化单体补齐连续性引用与执行证据。以下批次列出剩余范围，保留原编号用于关联讨论，每批完成领域实现、必要的协议适配和现状文档后再合入。

核心取舍是保留现有 loop、executor、policy、session 和 Runtime。数据从明确的所有者流向消费者，Context 只负责投影，不直接扫描 Runtime、任务目录或插件注册表。新增能力不依赖 middleware 框架，不增加后台服务，不增加模型判断型硬门禁。

连续性引用已实现，契约见[任务引用](../maintainers/ARCHITECTURE_BOUNDARIES.md#任务连续性引用)、[产物引用与查询](../maintainers/ARCHITECTURE_BOUNDARIES.md#产物引用与查询)、[技能接纳与引用](../maintainers/ARCHITECTURE_BOUNDARIES.md#技能接纳与引用)。剩余范围为 PR 6 的执行证据，保留原编号。

### 架构验收约束

- 每份可变状态只有一个事实来源；索引、投影和诊断均为派生值，不能反向覆盖权威数据。
- 优先删除重复路径、复用已有值类型与装配入口；不为各项能力分别引入 registry、manager 或事件仓库。
- 公共接口只暴露消费者真正需要的数据。可由已有 context 传递的执行身份不再扩展 RunHost 或绕经各适配器复制一套状态。
- 不对无法获知的完整行为建立虚假指纹。已有 prefix/model/usage 能解释的问题先复用，不新增长期维护的全量 manifest 副本。
- 每批评审同时检查依赖方向、状态数量和新增抽象的必要性。新增缓存必须有上限、失效与重建方式；新增抽象必须替换重复实现或承载明确的跨模块契约。
- 架构测试至少阻止 Context 反向依赖 Agent/Policy/Session、文件工具依赖 Policy，以及底层依赖 Runtime/交互层。并发稳定性用取消、steer、resume 和竞态检查验收。

### 第一轮不纳入的能力

- 自动 memory 抽取、向量数据库、跨会话语义召回。
- 显式 goal 自动续跑、调度器、持久 batch、分布式 lease。
- 任意插件 middleware 注入，或更换模型/图执行框架。
- MCP 动态 schema promotion；继续保留 capability 固定入口。
- 全局跨会话容量服务。当前架构约定一个应用一个 Runtime、Bot/Agent 拒绝并发运行，现有 task worker pool 继续负责子任务并发。
- 重新实现累计 token/step budget；其设计唯一归属 RUN_BUDGET.md。
- 默认 policy 判断“测试充分”“任务完成”或“报告可信”。

这些范围外事项不形成隐藏依赖。只有出现跨会话并发、跨重启无人值守或自动记忆的具体使用需求时，由对应 Runtime/Context 维护者单独提出设计。

## 2. 总体结构与状态所有权

| 待新增数据 | 权威来源 | 生命周期 | 持久化/恢复规则 |
| --- | --- | --- | --- |
| ExecutionReceipt | executor/工具结果接纳边界 | 单次调用 | append-only 证据；失败和未知结果不补造成功 |

禁止形成反向依赖：Runtime 不 import bot/core；Context 不 import session；文件工具不 import policy。需要跨层协调时由 bot/core/Agent 注入回调或值对象，并在锁外调用。

## 3. 剩余批次怎么推进

### PR 6：执行证据与子代理交接

**已有基础：** ToolCallResult、有效工具身份 resolver、OutputStore、StepEvent、录制、子代理 submit_result 和 attempt transcript。

**改动方案：**

1. 在实际执行边界创建稳定 receipt ID，身份使用宿主生成 UUID 并携带 run/attempt/call 关联；不使用 `[r1]` 这种位置编号。每次物理执行单独关联，审批拒绝不伪装成执行成功。
2. 状态区分 success、error、blocked、cancelled、unknown；未实现结构化状态的自定义工具只能表达其真实 error/nil-error 结果，不能推导命令 exit code 或业务成功。
3. receipt 记录有效工具名、参数摘要、执行前后时间、已知结构化错误/exit code、artifact 引用、执行边界返回值摘要，以及最终可见结果摘要和变换类型。不得从 Shell 文本或最终回答中提取“执行过测试”。
4. “执行边界返回值”只指工具交给 executor 的 ToolOutput；如果工具内部已裁剪或捕获达到上限，显式标 raw_complete=false，不能称完整原始进程输出。
5. receipt 随工具结果接纳进入 session/recording，存储一份权威事件，通过稳定 ID 查询。Context 只保存引用并可投影最近 16 条，正文总额受连续性投影预算约束。新增只读 `evidence` list/inspect，查询限当前 session 与获授权的 child attempt，分页最多 20 条。
6. 子代理 submit_result 增加可选 evidence IDs。首版不强制历史/自定义代理提交引用；父 agent 可收到 resolved/unknown/wrong-scope/error 结果。核对只证明引用与执行记录关联，不能称 verified 或 task satisfied。
7. 工具执行后 receipt 写入失败：保留已产生的结果和存储错误，终态标证据不完整，不自动重试工具。恢复发现未知结果也不生成成功 receipt 或盲重放。该行为复用现有“已提交事实与收尾失败分离”的错误契约。
8. 结束原因复用现有 RunSummary，统一主/子路径及 recording/headless 投影；预算原因归 RUN_BUDGET.md，不能额外引入一套 stop enum。仅有 receipt 不触发自动续跑或交付验收硬门禁。

**主要落点：** protocol、tool runtime/core、runner 的执行与结果预算路径、shell 结构化结果、Agent 结果入上下文、subagent handoff、session、Runtime payload codec/recording、headless/ACP 的已知载荷投影和 evidence 工具。

**验收：** 同名 provider call ID 跨 attempt 不混淆；capability 记录有效 MCP 目标；清理/截断后的可见摘要可核对；取消和存储失败不补造成功；压缩后 receipt ID 稳定；子代理引用不能越 scope；旧客户端可忽略新增可选字段。

**工作量/风险：** 大；尤其要避免引入第二套事件仓库和让证据失败触发副作用重放。先交付领域与记录契约、查询及最小协议兼容，不增加 GUI/TUI 专用仪表盘。

## 4. 依赖、数据兼容和回退

### 依赖顺序

- PR 6 复用现有执行身份、已有的配置诊断与已实现的引用/查询模式。
- RUN_BUDGET.md 可独立推进；若先合入，PR 6 接入其计数与停止语义；若尚未实现，不在本计划临时复制预算实现。

### 外部依赖

不新增 API key、token、第三方账户、MCP server、数据库或 CLI。离线验收使用 fake provider，不访问网络。不要求读取用户实际 config.json 或凭据文件；模型窗口从运行中已解析配置注入。

### 数据格式

第一轮保留 session FormatVersion=3，不改历史日志布局。新增字段均可选并有自己的 schema version；未知字段由旧程序忽略。新代码不能依赖索引存在才能恢复会话。receipt 等历史附加信息在旧版重写数据后可能丢失，这种降级要在升级说明中明确，不能承诺旧版无损重写。

持久化前仍走现有原子保存/备份边界。新索引不是文件访问授权；旧 ReadFiles 和旧 receipt 不能恢复为当前版本写入许可。

新增 Runtime 载荷统一登记 codec，覆盖记录回放、headless 和 ACP 已知事件解析；若必须新增必填字段或改变已有含义，必须升级相应外部契约，不以“JSON 可忽略字段”掩盖破坏性变更。本计划推荐只加可选字段和新事件，保留现有字段含义。

### 回退

- PR 6：旧代码忽略附加证据；不改工具副作用。证据历史丢失不能重新执行命令来“重建”。

不新增用户可见的总开关或一组长期 feature flags。每个提交可以单独回退；语义正确性的变更按版本生效。

## 5. 验证与合入标准

### 每批必要验证

1. 先建立能区分旧行为与新行为的可控场景，再实现；避免只验证字段存在。
2. 运行变更包的测试与 `go test ./internal/architecture`。
3. 触及协议/生命周期时运行 `go test ./acp ./headless ./runtime/... ./bot/agent/... ./bot/core`；文件与连续性变更增加相关 contextmgr/session/tool runtime 包。
4. 文件并发、取消、索引事件合并和 recording 变更运行对应包的 `go test -race`。
5. 进入发布前运行 `go test ./...`；evals 为独立 Go module，在其目录运行离线假模型回归。真实模型 eval 仅作为补充效果测量，不承担结构正确性的唯一证据。

### 关键场景矩阵

| 场景 | 必须保持的结果 |
| --- | --- |
| 连续两次压缩、会话恢复与模型切换 | 任务/技能/产物引用保留，投影受预算约束 |
| child resume 与迟到终态事件 | 同 attempt 不降级，新 attempt 可运行，旧事件不覆盖新状态 |
| 工具成功后持久化失败、未知执行结果恢复 | 保留事实与错误，不自动重放、不伪造 receipt |
| 原始/模型可见结果不同、provider call ID 重复 | 变换来源可追溯，引用不会串到其他 attempt |
| 10 倍任务/产物量 | 投影和查询保持上限，重建不全量加载正文 |

### 效果与成本记录

每批保留可比较的 fixture 和装配身份，记录配置变化定位、委派/技能/产物引用恢复、重复副作用、provider 调用次数、输入输出 token、延迟和缓存变化。

离线契约验收要求：配置摘要不含凭据、引用不越权、未知副作用不重放、索引更新不覆盖权威数据。模型语义保留率不能从假模型推断，应由独立 eval 报告；出现明显退化时先修正该批设计，不通过增加默认提示或放宽门禁掩盖。

## 6. 关键假设和审查关口

本方案假设**现有 transcript、task metadata 和工具产物能作为可查询的权威来源**。该假设对连续性和 receipt 的收益最关键。因此恢复时先检查源记录可用性；来源缺失就标 unavailable，绝不由模型补造。证据查询必须在来源损坏、被删除和旧版回写的夹具上验收。

合入前按以下关口评审：单一事实来源和分页、副作用与证据持久化顺序。没有新增外部依赖或凭据前置条件。

## 7. 完成后的文档处理

每完成一批，将实际行为写入 ARCHITECTURE_BOUNDARIES、ARCHITECTURE、必要的 TOOL_GUIDE/AGENT_GUIDE、SESSION_GUIDE/DATA_DIRECTORY 和协议现状文档，再移除本计划对应未完成条目。

RUN_BUDGET、SUBAGENT_NEXT 与本计划各保留单一职责；本计划不复制预算实现或任务 UI 功能。全部完成后删除此计划并更新两个目录索引；调研中的候选建议同步删去已完成项或由现状文档替代。
