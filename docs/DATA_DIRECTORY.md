# `~/.nekocode` 目录架构与设计说明

本文说明 NekoCode 在磁盘上留下什么、为什么这样划分，以及哪些目录可以安全删除。

## 两级根目录

NekoCode 使用两个互不相同的 `.nekocode` 根：

| 根 | 位置 | 内容 | 生命周期 |
|----|------|------|----------|
| **用户级** | `~/.nekocode/` | 会话、运行记录、日志、凭据、缓存 | 跨项目，跟随用户 |
| **项目级** | `<项目根>/.nekocode/` | 该项目专属的索引、扩展、MCP 定义、权限 | 跟项目走，可入库 |

项目级目录的位置取决于使用它的组件，两者的判定不同：

- **项目配置**（`NEKOCODE.md`、`.nekocode/.mcp.json`）由 `bot/project` 使用**当前工作目录**，不向上搜索。
- **代码索引**（`.nekocode/index.db`）由索引管理器从当前目录**向上查找**项目标记文件来确定根。

多数情况下两者指向同一目录（在项目根启动）；从子目录启动时，配置仍以该子目录为基准，索引则会归到上层项目根。

用户级目录由 `util/fs.NekocodeHome()` 固定返回 `$HOME/.nekocode`。

路径常量集中在 `util/fs/fs.go`：

```go
func NekocodeHome() string                 // ~/.nekocode
func NekocodeLogDir() string               // ~/.nekocode/logs
func NekocodeDataDir(subdir string) string // ~/.nekocode/<subdir>
func NekocodeDirs(subdir string) []string  // 项目级 + 用户级，按此顺序
```

`NekocodeDirs` 返回**两份**，这是扩展（`plugins`、`skills`）的设计：项目可以覆盖或补充用户级配置，两者叠加生效。

## 用户级目录

```
~/.nekocode/
├── sessions/      会话：快照、完整对话、该会话的运行事件流
├── checkpoints/   文件改动检查点，用于回退
├── exports/       上下文导出
├── logs/          运行日志、panic 日志
├── credentials/   MCP 等外部服务的凭据
├── sandbox/       工具执行的沙箱工作区
├── tmp/           临时文件（图片附件等）
├── plugins/       (可选) 用户级扩展
└── skills/        (可选) 用户级技能
```

### `sessions/` — 会话

一个会话的所有数据都在自己的目录下：

```
sessions/<session-id>/
├── session.json      快照：元数据、累计 token、上下文及消息文件水位
├── messages.0.json   当前上下文双槽之一（v3）
├── messages.1.json   当前上下文双槽之一（v3）
├── transcript.jsonl  追加式完整对话（每行一条消息）
└── runs/             该会话的运行事件流
    └── run_<N>_<uuid>/
        └── events.jsonl
```

`<session-id>` 格式为 `20060102T150405-<9 位纳秒>`，**UTC 时刻**。session.json 与 transcript.jsonl 分离是为了让快照保持紧凑：历史越长，`session.json` 也不会跟着膨胀。

v3 会话将当前上下文放在 `messages.0.json` / `messages.1.json`，`session.json` 只记录活动槽、消息数和 SHA-256。上下文变化时先写非活动槽，再原子切换 `session.json`；仅 token 等元数据变化时无需重写消息。读取旧 v2 内联格式不受影响，下次保存自动迁移。完全无变化的保存不会刷新文件或 `updated_at`。

`session.json` 里的累计字段（`prompt_tokens`、`cache_hit_tokens` 等）是整个会话的总量，不是逐轮数据。

**为什么 runs 放在会话里**：删掉一个会话应当带走它的全部数据；把 runs 放在旁边意味着两处都要清理。目录名本身就是会话标识，外部工具按会话查找运行时不需要任何推断。

### `tool-results/` — 工具原始输出

超出返回预算的工具结果保存在 `sessions/<id>/tool-results/result-*.txt`；子 Agent 使用其下稳定的 `subagents/<subagent-id>/` 子目录。文件默认 `0600`，新建目录默认 `0700`，会话删除时随会话目录清理。

活动消息的 `tool_output` 保存文件路径和不完整标记，`deferred_content` 保存因上下文不足而尚未交付的有界预览。预览可能包含原始文件没有的错误说明，因此恢复直接使用该预览，不从文件重新推导交付文本。未使用延迟机制的历史消息无需这些字段；缺少有效恢复字段的延迟消息会明确报错，不猜测或解析提示文案。

### `subagents/` — 子任务会话

标准 Bot 派发的子任务现在独立保存上下文和完整对话，归属于父会话，而不是某一轮父 run：

```
sessions/<parent-session-id>/subagents/<subagent-id>/
├── session.json      任务状态、当前及历史 attempts、各轮结果和上下文指针
├── messages.*.json   当前上下文双槽（v3）
├── transcript.jsonl  子任务输入、模型消息、工具调用和结果
├── transcript.idx    按需建立的可重建历史索引
├── observation.json  可重建的轻量任务摘要缓存
└── .active.lock      执行期间持有的操作系统独占锁
```

子任务工具输出使用 `sessions/<parent-id>/tool-results/subagents/<subagent-id>/` 稳定命名空间；同一任务恢复时可继续引用原有输出文件。父 run 的事件录制仍保留现有格式，尚未拆出独立子 run 事件文件。

子快照复用父会话的 transcript 水位提交机制。在任务开始、模型响应后、工具批次结束及最终退出时保存；无法保存时停止后续工具执行并返回错误。进程退出会释放锁，重新加载时将遗留的 `running` 解释为 `interrupted`。首次 checkpoint 在 transcript 追加后、快照提交前崩溃时，合法 tail 会归入独立的 interrupted 恢复轮次，不会落在轮次边界之外。恢复上下文会将未确认完成的工具调用补为“结果未知”，不会自动重放。

子任务正文和快照随父会话保留，不受 `runs/` 的事件过期策略影响，删除父会话时一起删除。子任务不进入顶层 `/sessions` 列表。

Runtime 已提供当前或指定父会话的任务列表、按任务分页读取 attempts 和完整记录，以及按 `SubAgentID` 过滤实时事件。任务列表只携带最近 attempt 的有界摘要，完整 attempts 按单任务分页查询。列表优先读取 `observation.json`，以源快照大小和修改时间校验；缓存缺失或失效时回退读取快照中的元信息，不恢复活动上下文。单条记录损坏显示为不可用，不隐藏其他任务。

详情查询只读快照确认水位内的 transcript，使用现有索引定位；索引缺失、损坏或落后时回退扫描。查询不修复日志、不更新索引，也不读取尚未提交的尾部，单页目标预算为 1 MiB（单条超大记录可独立返回）。

TUI 可通过 `Ctrl+G` 或 `/tasks` 打开子任务浮层，按需加载任务列表、轮次摘要、对话历史和工具输出。执行中默认显示紧凑状态，完成后默认显示当轮最终结果；`p` 展开过程，`[` / `]` 切换轮次。工具原始输出每页读取最多 64 KiB，只能访问该子任务已提交结果所引用的文件。

`task` 工具通过可选 `resume_id` 继续当前父会话内已保存的任务，沿用任务编号、历史与输出目录。必须保留原 profile，省略 skills 时复用上次保存的技能；指令和权限按当前配置重新构建。运行中的任务拒绝重复调用。每次正式执行保存独立 `attempt_id`、递增 `attempt_seq`、输入、结果、用量和 transcript 范围；旧版记录不会反推历史轮次，首次续跑才创建明确的新 attempt。只读 `task_list` 可查询当前父会话的任务摘要。父会话级全局目录索引尚未接入。

同批次子任务通过独立 worker 池执行，默认最多并发 8 个，可在 `config.json` 中通过 `max_concurrent_subagents` 配置，超出的任务保留在内存队列中。排队时只预留任务编号，不占执行槽，也不会提前创建子会话快照；排队状态通过父 run 的生命周期事件观测。取消或父 run 结束会终止等待状态。队列不是可恢复的作业系统，进程重启不会自动重放尚未开始的调用；已开始任务的上下文仍按上述规则保存。

### `runs/` — 运行事件流

每个 `run_` 是一次 turn（一次用户输入到最终回答）。新运行使用 `run_<N>_<uuid>`：数字是进程内递增、启动时接续近期录制的序号，随机后缀避免并发进程生成相同运行 ID。旧的 `run_<N>` 仍可读取。事件包含 `run_started`、`metrics_updated`、`tool_started`/`tool_completed`、`run_summary`、`run_done` 等类型，是逐轮的**权威**数据：token 用量、工具调用、耗时、结束原因都在这里。

**保留策略**：启动恢复后清理一次；持续运行时，在一轮结束后至多每分钟清理一次。以单个 run 的 `events.jsonl` 最后修改时间计，先删除超过 30 天（`RetentionAge`）的记录，再按最旧优先裁剪直到总量不超过 500 MB（`RetentionBytes`）。裁剪只删除 `sessions/<id>/runs/<run-id>/`，不删除会话快照或对话正文。

活动 run 持有操作系统文件锁，其他进程的清理也会跳过它；进程异常退出会自动释放锁。活动数据本身超过上限时允许暂时超限，结束后再回收。持续续用同一会话不会保护其中已经结束的旧 run。

**录制时还不知道会话是谁**：runtime 在运行对外可见前，通过 `BeginRun(runID, sessionID)` 注册录制归属。若会话要到 Bot.Run 内才创建，该 run 暂存于独立临时目录，结束时只迁移这一个 run：

```
临时：sessions/<provisional-id>_<run-id>/runs/<run-id>/
归位：sessions/<session-id>/runs/<run-id>/
```

事件始终按 RunID 查询固定目标。切换会话只影响新 run，不改变旧 run 的写入位置；启动后立即追加的输入也会写入正确目录。每条消息重启一次进程的前端（如 wecom-bot）仍能把同一会话的多轮写进同一个目录。

**1.0 之前的旧布局**把录制平级放在 `~/.nekocode/runs/`，与会话无关联。那些目录无法自动归位：事件流里不含 session id 或工作目录，`sequence` 是进程内序号（每个目录都从 1 开始），唯一可用的信号是“目录名与会话创建时刻同秒”——这只对进程首次启动成立，`--resume` 产生的目录没有任何关联依据。旧目录保持原样即可：数据完整，只是不能被按会话检索。

### `checkpoints/` — 回退

```
checkpoints/<session-id>/<turn>/
└── manifest.json     该轮开始前的文件状态
```

按会话和轮次记录文件改动前的快照，支撑 rewind。删除后无法回退到历史轮次，但不影响会话本身。

### `exports/` — 上下文导出

默认产物 `exports/nekocode-context.json`，用于把当前上下文交给外部工具。可随时重新生成。

### `logs/` — 日志

所有运行日志、调试日志、panic 日志都在这里，不写 `/tmp`。这是刻意选择：`/tmp` 在容器和重启后可能被清空，排查崩溃需要日志留存。

### `credentials/` — 凭据

```
credentials/mcp/<sha256>.json
```

文件名是密钥的哈希，不落明文标识。**权限敏感，不要提交或分享。**

### `sandbox/` — 沙箱

工具执行时的临时工作区。可安全删除。

### `tmp/` — 临时文件

```
tmp/images/<session-id>/*.png
```

粘贴的图片附件。发送后随会话删除而清理。可安全删除。

## 项目级目录

```
<项目根>/.nekocode/
├── index.db         代码索引（SQLite）
├── .mcp.json        项目级 MCP 服务定义
├── permissions.json 项目级工具权限
├── plugins/         项目级扩展
└── skills/          项目级技能
```

这些通常应该**纳入版本控制**（`index.db` 除外），让团队共享同一套项目配置。

项目指令则写在项目根的 `NEKOCODE.md`，不在 `.nekocode/` 内。

## 数据流

```
用户输入
  → sessions/<id>/transcript.jsonl   完整对话（追加，长期保留）
  → sessions/<id>/runs/run_N/        逐轮事件流（保留 30 天 / 500 MB）

工具改动文件
  → checkpoints/<id>/<turn>/         回退用快照
```

同一个会话下有两类数据，寿命不同：`transcript.jsonl` 回答“我们聊了什么”，长期保留；`runs/` 回答“那次跑得多快、用了多少 token、调了哪些工具”，是可裁剪的近期窗口。放在同一目录是为了内聚，裁剪策略不同则是各自的实现细节。

## 可删除性

| 目录 | 删除后果 |
|------|----------|
| `sessions/*/runs/` | 丢失 token/耗时/工具统计，且**无法恢复**——transcript 不含这些数据；会话内容本身不受影响 |
| `checkpoints/` | 无法 rewind 到历史轮次 |
| `tmp/` | 仅丢失未发送的图片附件 |
| `sandbox/` | 无影响，会自动重建 |
| `exports/` | 可重新导出 |
| `logs/` | 丢失历史日志 |
| `sessions/<id>/session.json`、`messages.*.json`、`transcript.jsonl` | **丢失对话内容本身，不可恢复** |
| **`credentials/`** | 需要重新授权 MCP 服务 |

## 与外部工具的接口

`sessions/<id>/runs/` 是为外部消费者设计的：事件格式稳定、自描述，且路径里带着会话标识，直接按会话查找即可，无需推断。

消费方需要注意两点：

1. **`runs/` 会过期**。超过 30 天的录制会被删除，因此适合做近期统计，不适合当作长期归档。
2. **1.0 之前的数据在 `~/.nekocode/runs/`**，与会话无关联。需要兼容旧数据时，按目录名是否带 `Z` 后缀区分：带 `Z` 的是旧批次，不带的是会话目录。

### 大会话历史读取

TUI 的历史浏览按原始消息序号从 `transcript.jsonl` 分页读取，初始请求最近 100 条原始记录（不是 100 个 turn），在窗口顶部继续向上滚动读取更早记录，底部向下返回较新记录。窗口切换会释放上一页内容。工具结果可能附带其所属 assistant 工具调用头，因此显示层收到的记录数可能略大于分页数量。

`transcript.idx` 是可重建的定长偏移索引；首次打开旧会话会扫描 JSONL 建立索引，之后按偏移读取。删除索引不会删除历史。普通分页遇到原文损坏会返回错误；恢复会话只允许修复未确认的末尾残片。

每页按约 1 MiB 原始数据限制，单条超大记录允许单独读取；正文、推理和工具参数的历史预览最多约 64 KiB，完整内容可通过 `/export` 流式导出。TUI 根据源文本和渲染缓存的估算字节数缩小窗口，目标为 8 MiB；这不是进程 RSS 硬上限。当前运行的输出和模型所需的活动上下文仍需驻留内存，运行结束后会刷新最近窗口。

运行日志启动时只恢复最近一次运行，旧运行在明确查询时读取。事件历史缓存和已结束运行缓存分别使用约 8 MiB 的估算预算，当前运行保留。旧的全历史 API 和 offset 分页接口为兼容仍可能读取完整历史；上述按需读取适用于新的 TUI 游标路径。
