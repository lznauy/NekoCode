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
├── session.json      快照：system prompt、累计 token、上下文水位
├── transcript.jsonl  追加式完整对话（每行一条消息）
└── runs/             该会话的运行事件流
    └── run_<N>_<uuid>/
        └── events.jsonl
```

`<session-id>` 格式为 `20060102T150405-<9 位纳秒>`，**UTC 时刻**。session.json 与 transcript.jsonl 分离是为了让快照保持紧凑：历史越长，`session.json` 也不会跟着膨胀。

`session.json` 里的累计字段（`prompt_tokens`、`cache_hit_tokens` 等）是整个会话的总量，不是逐轮数据。

**为什么 runs 放在会话里**：删掉一个会话应当带走它的全部数据；把 runs 放在旁边意味着两处都要清理。目录名本身就是会话标识，外部工具按会话查找运行时不需要任何推断。

### `tool-results/` — 工具原始输出

超出返回预算的工具结果保存在 `sessions/<id>/tool-results/result-*.txt`；子 Agent 使用其下独立的随机子目录。文件默认 `0600`，新建目录默认 `0700`，会话删除时随会话目录清理。

活动消息的 `tool_output` 保存文件路径和不完整标记，`deferred_content` 保存因上下文不足而尚未交付的有界预览。预览可能包含原始文件没有的错误说明，因此恢复直接使用该预览，不从文件重新推导交付文本。未使用延迟机制的历史消息无需这些字段；缺少有效恢复字段的延迟消息会明确报错，不猜测或解析提示文案。

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
| `sessions/<id>/session.json`、`transcript.jsonl` | **丢失对话内容本身，不可恢复** |
| **`credentials/`** | 需要重新授权 MCP 服务 |

## 与外部工具的接口

`sessions/<id>/runs/` 是为外部消费者设计的：事件格式稳定、自描述，且路径里带着会话标识，直接按会话查找即可，无需推断。

消费方需要注意两点：

1. **`runs/` 会过期**。超过 30 天的录制会被删除，因此适合做近期统计，不适合当作长期归档。
2. **1.0 之前的数据在 `~/.nekocode/runs/`**，与会话无关联。需要兼容旧数据时，按目录名是否带 `Z` 后缀区分：带 `Z` 的是旧批次，不带的是会话目录。
