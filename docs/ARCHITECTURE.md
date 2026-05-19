# NekoCode 架构文档

> **本文档职责**: 描述项目架构——目录结构、包依赖、模块实现、代码层面的机制。不包含 UI 设计、交互设计、设计原则等属于 DESIGN.md 的内容。更新时请保持此边界。

## 项目概述

NekoCode 是一个基于 Go 的终端 AI 助手，使用 Bubble Tea v2 构建 TUI，支持多 LLM provider（Anthropic / DeepSeek，以及 OpenAI 兼容协议），具备 Agent 循环、Native Function Calling、工具执行、权限确认、Plan Mode 只读规划、Anthropic Prompt Caching、Auto-review 自动验证、微压缩、Session Memory 和上下文管理机制。

## 目录结构

```
nekocode/
├── main.go                         # 入口：无参→TUI 交互模式，有参→单次 CLI 模式
├── common/                         # 公共类型与工具
│   ├── types.go                    #   共享类型定义
│   ├── confirm.go                  #   确认类型
│   └── util.go                     #   通用辅助函数
├── llm/                            # LLM 抽象层
│   ├── llm.go                      #   LLM 接口、Message/Response/ToolDef 等核心类型
│   ├── deepseek.go                 #   DeepSeek 专用实现（OpenAI 兼容协议）
│   ├── anthropic.go                #   Anthropic 实现（tool_use/tool_result 双向转换 + SSE 流式）
│   ├── factory.go                  #   NewClient / Clone — provider 感知的工厂函数（支持 anthropic/deepseek）
│   └── retry.go                    #   指数退避重试（IsRetryable — HTTP 状态码判定 + Retry）
├── bot/                            # 核心逻辑
│   ├── bot.go                      #   Bot 结构体、依赖注入
│   ├── api.go                      #   公开 API（SendMessage、Abort 等）
│   ├── agent/                      #   Agent 循环
│   │   ├── agent.go                #     Agent 结构体 + New() 构造函数
│   │   ├── run.go                  #     Run() 主循环：Reason→Execute→Feedback
│   │   ├── run_exec.go             #     Run() 中的工具执行集成
│   │   ├── reasoner.go             #     Reason()：LLM 调用 + 流式回调
│   │   ├── retry.go                #     Agent 层重试逻辑
│   │   ├── log.go                  #     日志
│   │   ├── budget/                 #     预算与配额管理
│   │   │   ├── exploration.go      #       探索螺旋检测（连续只读→强制红配额）
│   │   │   └── quota.go            #       每轮工具配额计算
│   │   └── subagent/               #     子 Agent 系统
│   │       ├── agents.go           #       子 Agent 类型定义
│   │       ├── engine.go           #       子 Agent 执行引擎
│   │       ├── registry.go         #       子 Agent 注册表
│   │       ├── classify.go         #       安全分类器（子 Agent 结果危险模式检测）
│   │       ├── result.go           #       子 Agent 结果类型
│   │       └── prompts/            #       子 Agent 专用 prompt
│   │           ├── explore.md      #         explore agent prompt
│   │           ├── verify.md       #         verify agent prompt
│   │           ├── executor.md     #         executor agent prompt
│   │           ├── plan.md         #         plan agent prompt
│   │           ├── decompose.md    #         任务分解 prompt
│   │           ├── shared_rules.md #         共享规则
│   │           └── output_format.md#         输出格式规范
│   ├── config/                     #   配置管理
│   │   └── config.go               #     Config 结构体 + Load()（~/.nekocode/config.json）
│   ├── command/                    #   斜杠命令系统
│   │   ├── parser.go               #     Parser + Command + Callbacks
│   │   └── lifecycle.go            #     SummarizeIfNeeded / ForceSummarize / ContextStats / ForceFreshStart
│   ├── ctxmgr/                     #   上下文管理
│   │   ├── manager.go              #     Manager：Build() 上下文组装入口
│   │   ├── storage.go              #     上下文持久化存储
│   │   ├── report.go               #     上下文统计报告
│   │   ├── compact/                #     压缩子系统
│   │   │   ├── compact.go          #       AutoCompactIfNeeded() 入口
│   │   │   ├── compactor.go        #       Compactor 核心逻辑
│   │   │   ├── levels.go           #       五级预警阈值定义
│   │   │   ├── micro.go            #       微压缩（MicroCompact）
│   │   │   ├── budget.go           #       工具结果预算（grep 截断）
│   │   │   ├── collapse.go         #       消息折叠
│   │   │   ├── merge.go            #       摘要合并
│   │   │   ├── prompt.go           #       压缩 prompt 模板
│   │   │   ├── snipe.go            #       冷历史消息批量删除
│   │   │   └── log.go              #       压缩日志
│   │   ├── context/                #     上下文内容定义
│   │   │   ├── content.go          #       Content 结构体（system/tools/anchor/todo/messages）
│   │   │   └── meta.go             #       元数据
│   │   ├── memory/                 #     Session Memory 提取
│   │   │   └── memory.go           #       Memory 结构体 + 提取逻辑
│   │   └── token/                  #     Token 估算
│   │       ├── estimate.go         #       Token 估算器（ASCII ~4/token, CJK ~1.5/token）
│   │       └── tracker.go          #       Token 使用追踪
│   ├── hooks/                      #   生命周期钩子系统
│   │   ├── hooks.go                #     Hook 接口定义
│   │   ├── builtin.go              #     内置钩子实现
│   │   ├── inject.go               #     钩子注入逻辑
│   │   └── stop.go                 #     停止条件钩子
│   ├── projctx/                    #   项目上下文
│   │   ├── project.go              #     项目上下文结构体
│   │   └── index.go                #     项目索引（NEKOCODE.md 发现 + @include 递归）
│   ├── prompt/                     #   System Prompt 构建
│   │   ├── builder.go              #     Prompt 构建器
│   │   ├── plan.go                 #     Plan Mode prompt
│   │   ├── system.md               #     英文 system prompt 模板
│   │   └── system_zh.md            #     中文 system prompt 模板
│   ├── session/                    #   Session 管理
│   │   └── session.go              #     Session 结构体 + 持久化
│   ├── skill/                      #   技能系统
│   │   ├── skill.go                #     Skill 接口定义
│   │   ├── loader.go               #     Skill 加载器（YAML 解析 + 目录发现）
│   │   ├── tool_skill.go           #     技能工具注册
│   │   └── bundled/                #     内置技能
│   │       ├── bundled.go          #       内置技能注册
│   │       └── meta/               #       技能元数据
│   │           └── SKILL.md        #         skill-creator 技能定义
│   └── tools/                      #   工具系统
│       ├── types.go                #     Tool 接口 + Phase/Confirm 等核心类型
│       ├── executor.go             #     Executor：并行/串行调度、权限检查、输出截断、边界标记
│       ├── descriptor.go           #     工具描述符
│       ├── registry.go             #     工具注册表
│       ├── file_cache.go           #     文件读取缓存追踪
│       ├── util.go                 #     工具辅助函数
│       └── builtin/                #     内置工具实现
│           ├── register.go         #       RegisterAll() 注册入口
│           ├── tool_bash.go        #       Bash 执行
│           ├── tool_read.go        #       文件读取
│           ├── tool_write.go       #       文件写入
│           ├── tool_edit.go        #       文件编辑
│           ├── tool_glob.go        #       文件匹配
│           ├── tool_grep.go        #       内容搜索
│           ├── tool_list.go        #       目录列表
│           ├── tool_tree.go        #       目录树
│           ├── tool_task.go        #       子 Agent 任务
│           ├── tool_todo.go        #       Todo 管理工具
│           ├── tool_webfetch.go    #       Web 抓取
│           ├── tool_websearch.go   #       Web 搜索
│           ├── tool_project_info.go#       项目信息查询
│           ├── html2md.go          #       HTML→Markdown 转换
│           └── html2md_test.go     #       HTML→Markdown 测试
├── tui/                            # TUI 界面
│   ├── tui.go                      #   Model 定义 + Init()
│   ├── update.go                   #   Update() 消息分发
│   ├── view.go                     #   View() 渲染入口
│   ├── handlers.go                 #   事件处理器
│   ├── helpers.go                  #   辅助函数
│   ├── agent.go                    #   Agent 桥接
│   ├── model.go                    #   数据模型
│   ├── types.go                    #   类型定义
│   ├── components/                 #   UI 组件
│   │   ├── block/                  #     内容块渲染
│   │   │   ├── block.go            #       Block 接口 + 工厂
│   │   │   ├── block_text.go       #       文本块
│   │   │   ├── block_tool.go       #       工具块
│   │   │   ├── block_render.go     #       渲染逻辑
│   │   │   └── block_diff.go       #       Diff 渲染
│   │   ├── message/                #     消息项渲染
│   │   │   ├── message.go          #       MessageItem 接口
│   │   │   ├── message_user.go     #       用户消息
│   │   │   ├── message_assistant.go#       助手消息
│   │   │   ├── message_system.go   #       系统消息
│   │   │   ├── message_error.go    #       错误消息
│   │   │   ├── message_shared.go   #       共享渲染逻辑
│   │   │   └── markdown.go         #       Markdown 渲染
│   │   ├── processing/             #     处理中状态渲染
│   │   │   ├── processing.go       #       ProcessingItem 定义
│   │   │   ├── processing_render.go#       渲染逻辑
│   │   │   └── render_text.go      #       文本渲染
│   │   ├── messages.go             #     消息列表容器
│   │   ├── input.go                #     输入框
│   │   ├── header.go               #     顶部状态栏
│   │   ├── splash.go               #     启动页
│   │   ├── confirm_bar.go          #     确认栏
│   │   ├── list_widget.go          #     列表组件
│   │   ├── suggestions.go          #     Suggestions：斜杠命令自动补全
│   │   └── scrollbar.go            #     Scrollbar：独立滚动指示器
│   └── styles/                     #   样式
│       ├── colors.go               #     色彩体系 + Styles 结构体 + FmtTokens
│       └── charset.go              #     制表符字符集（含 ASCII 回退）
```

## 包依赖图

```
nekocode (main.go)
  ├── bot ──────────┬── agent ──────────┬── agent/budget
  │                 │                   ├── ctxmgr ──────────┬── ctxmgr/compact ─── ctxmgr/context + ctxmgr/token + llm
  │                 │                   │                   ├── ctxmgr/context ─── ctxmgr/token + llm
  │                 │                   │                   ├── ctxmgr/memory
  │                 │                   │                   ├── ctxmgr/token ─── llm
  │                 │                   │                   └── llm
  │                 │                   ├── hooks
  │                 │                   ├── tools ──────────┬── llm
  │                 │                   │                   └── common
  │                 │                   ├── tools/builtin ─── agent/subagent + projctx + tools + common
  │                 │                   └── llm
  │                 ├── agent/subagent ─── ctxmgr + ctxmgr/compact + ctxmgr/context + tools + llm + common
  │                 ├── command ────────── agent + ctxmgr + prompt + skill + tools
  │                 ├── config ─────────── (stdlib)
  │                 ├── ctxmgr ─────────── ctxmgr/compact + ctxmgr/context + ctxmgr/memory + ctxmgr/token + llm + common
  │                 ├── hooks ──────────── (stdlib)
  │                 ├── projctx ────────── (stdlib)
  │                 ├── prompt ─────────── ctxmgr/context
  │                 ├── session ────────── llm
  │                 ├── skill ──────────── tools + common
  │                 ├── skill/bundled ──── skill
  │                 ├── tools ──────────── common + llm
  │                 └── llm
  ├── tui ───────────┬── bot
  │                   ├── common
  │                   ├── components ──────── common + block + message + processing + styles
  │                   ├── components/block ──── styles
  │                   ├── components/message ── block + styles
  │                   ├── components/processing ─ block + styles
  │                   └── styles (stdlib + lipgloss + glamour)
  └── common (stdlib)
```

- `tools` 是整个系统的基础层：Tool 接口、Registry、Executor、Phase 类型、Confirm 类型
- `tools/builtin` 包含所有内置工具的具体实现，通过 `RegisterAll()` 注册；依赖 `agent/subagent` 用于子 Agent 任务分发
- `subagent` 与 `agent` 共享 `tools.Executor`，保证工具安全检查一致
- `config` 和 `command` 为独立子包，通过 `bot.go` 组装
- `session` Session 持久化，存储完整对话快照
- `skill` 技能系统，YAML 定义工作流，运行时引擎执行；`bundled` 子包提供编译进二进制的内置技能
- `projctx` 项目上下文预加载，NEKOCODE.md 发现与 @include 递归
- `prompt` System Prompt 构建，依赖 `ctxmgr/context` 获取上下文内容
- `hooks` 生命周期钩子系统，独立子包，通过 `bot.go` 组装
- `ctxmgr` 上下文管理分层：`token`（估算）→ `context`（内容定义）→ `compact`（压缩）→ `ctxmgr`（组装入口）
- `tui/components/block` 导出 `BuildToolGroups` 和 `ToolGroupInfo`，streaming 和 message 两边共用

## 核心架构：Agent 循环

```
用户输入
  │
  ▼
┌──────────────────────────────────────────────┐
│  Run() 主循环（默认无限制，可配置）          │
│                                              │
│  state = stepState{input}                    │
│                                              │
│  ┌─ for !finished && step < maxIterations ─┐ │
│  │                                          │ │
│  │  ① Reason(state) → ReasoningResult      │ │
│  │     ├─ / 命令 → ActionFinish             │ │
│  │     └─ callLLMForTool()                  │ │
│  │         ├─ AutoCompactIfNeeded() 看门狗   │ │
│  │         ├─ ctxMgr.Build(true) 组装上下文  │ │
│  │         ├─ llmClient.ChatStream() 流式   │ │
│  │         ├─ withRetry() 指数退避重试       │ │
│  │         └─ 解析 tool_calls / text        │ │
│  │                                          │ │
│  │  ② ExecuteBatch(calls) → results         │ │
│  │     ├─ partition(ro, mw)                 │ │
│  │     ├─ runParallel(ro) / runSequential(mw)│ │
│  │     ├─ PlanMode 检查 (write/edit 拒绝)   │ │
│  │     ├─ DangerLevel 检查 + confirmFn      │ │
│  │     ├─ read-before-write check           │ │
│  │     ├─ tool.Execute() → TruncateOutput() │ │
│  │     └─ MarkRead() tracking               │ │
│  │     └─ filesModified/didReproduce 追踪   │ │
│  │                                          │ │
│  │  ③ Feedback(state, result)              │ │
│  │     ├─ step++ / shouldStop               │ │
│  │     ├─ doomLoopCheck (4x same → stop)    │ │
│  │     ├─ detectDiminishingReturns          │ │
│  │     └─ 构建下一步 stepState              │ │
│  │                                          │ │
│  │  无 tool_calls 时:                       │ │
│  │  └─ Auto-review: filesModified→单次注入   │ │
│  │                                          │ │
│  │  ShouldStop? / doomLoop? → forceSynthesize()│ │
│  └──────────────────────────────────────────┘ │
│                                              │
│  返回 RunResult{FinalOutput, Steps}          │
└──────────────────────────────────────────────┘
```

## 上下文管理

### 五级预警 + 自动压缩

`AutoCompactIfNeeded()` 在每次 `Build()` 前运行，根据剩余 token buffer 触发不同级别操作：

| Level | 剩余 buffer | 动作 |
|-------|------------|------|
| Normal | > 44,800 | 无操作 |
| Warning | ≤ 44,800 | 无操作（仅告警） |
| MicroCompact | ≤ 35,200 | 触发微压缩 |
| Compact | ≤ 25,600 | 触发完整压缩（Session Memory → LLM Summarize） |
| Blocking | ≤ 6,400 | 拒绝新输入，强制压缩 |

阈值基于 64K budget，大 budget 下自动按比例缩放。

### 上下文锚点

`compact/snipe.go` 在压缩前标记关键消息——包含用户核心指令、系统约束、API 版本要求等的消息在压缩时优先保留，防止关键信息被误清除。

### 摘要验证

`compact/prompt.go` 在 LLM 生成摘要后执行二次校验：检查摘要是否保留了代码片段、错误信息、文件路径等关键内容，验证失败则重新生成。

### 微压缩

`MicroCompactIfNeeded()` 在每次 `Build()` 前调用，但仅在 token 超过预算 50% 时激活。将旧的 compactable 工具结果内容替换为 `[Old tool result cleared]`。保留数量按 token budget 分级：<64K→3、~64K→5、>=128K→8。大 budget 下保留更多结果，减少 prefix cache 失效。

防止大文件读取/命令输出导致上下文膨胀，同时模型随时可以重新执行工具获取内容。非 compactable 工具（task、todo_write）永不清除。

### 结构化摘要

token 超过预算 80% 时，`Summarize()` 将最旧的一半消息压缩为结构化摘要，包含：Goal、Progress、Key Decisions、Next Steps、Critical Context、Relevant Files。支持已有摘要的增量更新。

### Session Memory

Session 持久化到 `~/.nekocode/sessions/` 目录，保存完整对话快照（消息历史、token 用量、加载的 skills 等）。Memory 文件（`~/.nekocode/memory.md`）通过 Auto-Compaction 的 `<key-facts>` 块增量更新，包含 5 个 section（Tech Stack、Active Goals、Completed Tasks、Key Architecture Map、User Preferences）。`/new` 命令优先用 session memory 作为免费摘要。

### 滑动窗口 + Token 预算 + 前缀缓存优化

`Build()` 按三阶段输出消息数组，针对 DeepSeek automatic prefix caching 优化：

1. **Static prefix** — system prompt + anchor（永不变化）
2. **Message history** — 稳定前缀（旧消息不变，新消息追加）
3. **Dynamic suffix** — todoText + skillList + summary + tool hint（放入尾部，不影响 prefixes）

动态内容（todo、summary）放在消息历史之后，确保系统提示词 + 历史消息已缓存的前缀不被破坏。然后按 token 预算从头部动态修剪消息（保留最近 2 条用户消息的上下文），孤儿 tool 消息（无对应 assistant tool_call）被过滤，空内容非 system 消息替换为 `"."`。当 `withTools=true` 时，末尾追加工具选择提醒。

## 工具系统

### Tool 接口

```go
type Tool interface {
    Name() string
    Description() string
    Parameters() []Parameter
    ExecutionMode(args map[string]interface{}) ExecutionMode  // ModeParallel / ModeSequential
    DangerLevel(args map[string]interface{}) DangerLevel
    Execute(ctx context.Context, args map[string]interface{}) (string, error)
}
```

### 执行模式

| 模式 | 说明 | 工具 |
|------|------|------|
| `ModeParallel` | 可并发执行 | glob, grep, web_search, web_fetch, read, list, task |
| `ModeSequential` | 独占执行 | bash, edit, write, todo_write |

### 危险等级

```
LevelSafe        (0) — 只读，自动放行 (标签: safe)
LevelWrite       (1) — 可能修改，确认 (标签: modify)
LevelDestructive (2) — 破坏操作，确认 (标签: danger)
LevelForbidden   (3) — 永远拒绝 (标签: blocked)
```

bash 命令智能分级：匹配 `go version`、`git log`、`git diff`、`ls`、`cat`、`ps` 等纯输出命令时返回 `LevelSafe`。

### 路径安全

`write` 和 `edit` 通过 `validatePath()` 解析符号链接并返回绝对路径。跨工作目录的路径不再被拒绝——确认系统处理用户同意。

### 文件缓存

`GlobalFileCache`（`tools/file_cache.go`）在多次 read 调用间缓存文件内容。LRU 驱逐策略（100 条目 / 25MB 上限），基于文件 mtime + size 校验自动失效。子 Agent 自动共享同一缓存实例。缓存命中时直接返回缓存内容，避免重复磁盘 I/O。

### 安全门控

`executor.go` 中的 `TruncateOutput()` 为每个工具结果包裹 `--- BEGIN/END tool_result ---` 边界标记，包含工具名和调用 ID，帮助模型区分工具输出与对话/系统提示。

## 幻觉防治体系

基于纵深防御思想，在 6 个代码层面实现多层防幻觉机制（以下仅列出代表性机制）：

### 第 1 层：工具安全

| 机制 | 位置 |
|------|------|
| 危险等级四级分类（Safe/Write/Destructive/Forbidden） | `common/types.go` |
| bash 命令关键词智能分级（sudo/rm/kill → Forbidden/Destructive，ls/pwd → Safe） | `tools/builtin/tool_bash.go` |
| 路径验证 + 符号链接解析 | `tools/util.go` |
| 二进制文件检测（null 字节 + UTF-8 校验 + 可打印比例） | `tools/builtin/tool_read.go` |
| URL 校验 + 内网 IP 拒绝（SSRF 防护） | `tools/builtin/tool_webfetch.go` |
| WebFetch 重定向上限 5 次 | `tools/builtin/tool_webfetch.go` |
| Web 搜索结果上限 15 条 | `tools/builtin/tool_websearch.go` |

### 第 2 层：执行拦截

| 机制 | 位置 |
|------|------|
| DangerLevel 强制校验：LevelForbidden 直接拒绝，LevelWrite+ 需用户确认 | `tools/executor.go` |
| 先读后改强制：write/edit 前检查文件是否已 read，未读则拒绝 | `tools/executor.go` |
| 文件读取追踪（MarkRead/WasRead），跨子 Agent 共享 | `tools/executor.go` |
| 并行/串行分区调度 + worker pool 上限 10 | `tools/executor.go` |

### 第 3 层：输出完整性

| 机制 | 位置 |
|------|------|
| 工具结果边界标记（`--- BEGIN/END tool_result ---`） | `tools/executor.go` |
| 工具输出截断（2000行/50KB） | `tools/executor.go` |
| Web 内容截断（WebFetch 3000 runes，WebSearch 6000 runes） | `tools/builtin/tool_webfetch.go`, `tool_websearch.go` |
| Garbled tool call 检测（截断 XML/JSON 片段过滤） | `agent/reasoner.go` |

### 第 4 层：Agent 循环控制

| 机制 | 位置 |
|------|------|
| 末日循环检测：连续 4 次相同工具调用 → forceSynthesize | `agent/run.go` |
| 收益递减检测：连续 3 回合 completion token < 200 → 强制停止 | `agent/run.go` |
| Auto-review：文件修改后 → 单次注入 build+test 提示（verifyInjected 防重复） | `agent/run.go` |
| Bug 复现检测：bash 命令实际失败（exit≠0 或输出含 error/panic）→ didReproduce=true | `agent/run.go` |
| ContextTransform：工具结果 > 40 条时注入推进提示 | `bot/bot.go` |
| finish_reason=length 两级升级（Tier 1: max_tokens → 64000，Tier 2: 关 thinking） | `agent/reasoner.go` |
| LLM 调用指数退避重试（0.5s→1s→2s，最多 4 次）+ HTTP 状态码判定 | `agent/retry.go`, `llm/retry.go` |
| Plan Mode：`/plan` 命令限制为只读工具，防止未审批即执行 | `tools/executor.go`, `bot/bot.go` |
| maxIterations 0（无限制，可配置 `config.max_iterations`），适配长任务 | `agent/agent.go`, `config/config.go` |

### 第 5 层：上下文保真

| 机制 | 位置 |
|------|------|
| 关键约束锚定（7 个正则提取 "不要/必须/do not/must" 等，永不压缩） | `ctxmgr/compact/snipe.go` |
| 当前目标锚定（提取首条实质性用户消息 + session memory） | `ctxmgr/compact/snipe.go` |
| 每次 Add() 自动提取用户消息中的约束 | `ctxmgr/storage.go` |
| 摘要后约束验证 + 缺失时重新摘要（最多 1 次） | `ctxmgr/compact/prompt.go` |
| 摘要尾部保留最后 3 轮对话 | `ctxmgr/compact/compact.go` |
| 微压缩（50% 预算时清除旧工具结果，保留 3-8 条按 budget 分级） | `ctxmgr/compact/micro.go` |
| 五级自动压缩（Normal → Warning → Micro → Compact → Blocking） | `ctxmgr/compact/levels.go` |
| Build() 孤儿 tool 消息过滤 + 空内容兜底 | `ctxmgr/manager.go` |
| Token 估算（ASCII ~4/token, CJK ~1.5/token）+ API 校准 | `ctxmgr/token/estimate.go` |

### 第 6 层：LLM 调用控制

| 机制 | 位置 |
|------|------|
| Anthropic thinking budget 钳制（min(16000, maxTokens/2)，budget < maxTokens 强制） | `llm/anthropic.go` |
| DeepSeek 默认关 thinking（`"thinking": {"type": "disabled"}`） | `llm/deepseek.go` |
| `SetThinkingBudget` / `SetReasoningEffort` 跨 provider 互译（不再静默忽略） | `llm/anthropic.go`, `llm/deepseek.go` |
| 子 Agent thinking 强制关闭（注释："Sub-agents execute — they don't need extended reasoning"） | `bot/bot.go` |
| finish_reason=length 时关 thinking 释放全部 token 给输出 | `agent/reasoner.go` |
| Anthropic 开启 thinking 时 temperature 强制设为 1 | `llm/anthropic.go` |
| 流式 HTTP 客户端独立超时（10min），防止服务端 hang 导致 goroutine 泄漏 | `llm/llm.go` |

### 非代码层（prompt/设计级补充）

以下机制通过 system prompt 和子 agent 提示文本实现，属于设计级防护，不在此表中：

- System prompt 反幻觉指令（禁止生成 URL、忠实报告、先验证再声称完成）
- **Debugging 方法论**：6 阶段（Reproduce→Diagnose→Fix→Self-review→Verify→Adversarial）+ Anti-patterns 清单
- Claude prompt: think-first + plan-before-code；DeepSeek prompt: act-first + anti-overthinking
- Task Tracking 指令：3+ 步骤必须先建 todo_write 任务列表
- Tool description 精细化：bash/edit/write 含使用时机、反模式、常见错误
- 日期注入（防止时间幻觉）
- verify agent 格式强制 + 自检清单（VERDICT: PASS/FAIL/PARTIAL）
- Session memory 模板警告（"记忆说 X 存在 ≠ X 现在存在"）
- web_search/fetch 的 Sources 引用格式要求（prompt 文本，非代码强制）

## TUI 组件树

```
Model
├── Header         — provider/model · ↑tokens ↓tokens 🧹N
├── Splash         — 启动页 (ASCII 猫 + 猫眼闪烁)
├── Messages + Scrollbar — 消息列表 + 独立滚动指示器
│   ├── UserMessageItem        — 暖金 ▐ 粗条 "You"
│   ├── AssistantMessageItem   — teal ▐ 粗条 "Assistant" + ContentBlocks + Footer
│   ├── SystemMessageItem      — 蓝色 ▐ 粗条 "·"
│   ├── ErrorMessageItem       — 红色 ▐ 粗条 "!"
│   └── ProcessingItem         — teal │ ◉ spinner + Phase + 工具组 + Output + Reasoning
├── Suggestions    — 斜杠命令自动补全
├── Input          — 消息输入框（历史翻阅、tab 补全）
└── ConfirmBar     — 工具确认卡片
```

### 工具组折叠

`BuildToolGroups()` 将连续同名 `BlockTool` 分组为 `ToolGroupInfo`。`renderGroupLine()` 渲染组头 `◆ name ×N [+]`，edit 组展开时调用 `RenderEditGroupExpanded()` 内联每个文件的 diff（无嵌套折叠）。其他工具组展开时逐条渲染各子块并缩进 2 格。

Ctrl+E 触发 `toggleBlocks()`：检测可折叠组/独立 edit 块，取反 `Collapsed` 状态。`BuildToolGroups` 和 `RenderEditGroupExpanded` 在 `block_render.go` 和 `processing_render.go` 两侧共享。

### Markdown 渲染

`tui/components/message/markdown.go` 封装 glamour 库，使用 tokyo-night 主题。按终端宽度缓存 renderer 实例（40-160 字符），`Warmup()` 预创建常用宽度的渲染器以加速首屏显示。

### 输出噪声过滤

`processing/render_text.go` 的 `isEmptyOrNoise()` 检测纯空白、纯点号、纯符号行。全噪声时 `renderOutputSection()` 跳过渲染。

## 对话中的 IO

### BTW 中断

Agent 处理中用户可输入新消息打断当前 LLM 调用并注入上下文。`replaceCtx()` 使用 `parentCtx` 保持取消链。

### ShouldStop 断路器

`detectDiminishingReturns`（连续 3 回合 completion token < 200）和 `doomLoopCheck`（4 次连续相同工具调用）触发 `forceSynthesize()`。

### ContextTransform

`bot.go` 注册 `SetContextTransform`：当工具结果 > 40 条时注入 `[System] N tool results accumulated...` 提示，引导模型检查未完成子任务或调用 verify 验证。

### 指数退避重试

`llm/retry.go` 和 `agent/retry.go`：LLM 调用失败自动重试，指数退避 0.5s→1s→2s（最多 4 次尝试）。

### 确认机制

```
Agent goroutine                    TUI goroutine
  │                                  │
  ├─ executeOne()                    │
  ├─ level >= LevelWrite             │
  ├─ confirmFn(req) ────→ confirmCh  │
  │  (阻塞)               ↓          │
  │                    listenConfirm │
  │                       ↓          │
  │                    confirmMsg    │
  │                       ↓          │
  │                  ConfirmBar.View │
  │                  [enter]/[esc]   │
  │  ← req.Response ←───┘            │
  ├─ continue / deny                 │
```

## LLM 抽象层

### 接口

```go
type LLM interface {
    Chat(ctx, messages []Message, tools []ToolDef) (*Response, error)
    ChatStream(ctx, messages []Message, tools []ToolDef) (<-chan StreamToken, <-chan error)
    SetAPIKey(apiKey string)
    SetBaseURL(url string)
    SetMaxTokens(n int)
    MaxTokens() int
    SetDisableThinking(disable bool)
    SetThinkingBudget(tokens int)     // 0=default, -1=disabled, >0=custom (Anthropic)
    SetReasoningEffort(effort string) // "high"/"max" (OpenAI compat)
}
```

### 共享 HTTP 客户端

`llm.go` 定义三个共享客户端，共用同一个连接池：

| 变量 | 超时 | 用途 |
|------|------|------|
| `SharedHTTPClient` | 无 | 保留（向后兼容） |
| `SharedHTTPClientTimeout` | 120s | `Chat()` 同步调用 |
| `SharedHTTPStreamClient` | 10min | `ChatStream()` 流式调用 — 防止服务端 hang 导致 goroutine 泄漏 |

### Provider 适配

| Provider | 实现文件 | 要点 |
|----------|---------|------|
| DeepSeek | `deepseek.go` | `/chat/completions`（OpenAI 兼容协议），`thinking: {type: "disabled"}` 默认关闭，`reasoning_effort` 控制；`SetThinkingBudget` 自动映射到 `reasoning_effort` |
| Anthropic | `anthropic.go` | `/v1/messages`，SSE 解析，默认 `thinking: {type: "adaptive"}`，支持 `budget_tokens`；`SetReasoningEffort` 自动映射到 thinking 配置；**Prompt Caching**：3 个 `cache_control: {type: "ephemeral"}` 断点（system/tools/messages），`anthropic-beta: prompt-caching-2024-07-31` 请求头，`cache_read_input_tokens` / `cache_creation_input_tokens` 用量追踪 |

## 模块职责

| 模块 | 位置 | 职责 |
|------|------|------|
| **Agent 循环** | `bot/agent/` | Reason→Execute→Feedback，BTW 中断，指数退避重试，token 统计 |
| **子 Agent** | `bot/agent/subagent/` | 独立循环，thinking 禁用，共享 tools.Executor |
| **LLM 网关** | `llm/` | 统一对接多 provider，共享 HTTP 连接池（含流式专用超时），流式解析，指数退避重试 |
| **工具系统** | `bot/tools/` | Tool 接口 + Executor + DangerLevel + 路径安全 + Phase 类型 |
| **内置工具** | `bot/tools/builtin/` | 14 个内置工具的具体实现，通过 RegisterAll() 注册 |
| **上下文管理** | `bot/ctxmgr/` | 五级预警 + 滑动窗口 + 微压缩 + 锚点 + 摘要验证 |
| **Session Memory** | `bot/session/` | Session 持久化 + Memory 文件增量更新 |
| **Skill 系统** | `bot/skill/` | YAML 技能定义 + 发现 + 注入 + 运行时引擎 |
| **内置 Skill** | `bot/skill/bundled/` | 编译进二进制的内置技能（go:embed） |
| **项目上下文** | `bot/projctx/` | NEKOCODE.md 发现 + @include 递归加载 |
| **Bot 组装** | `bot/bot.go` | 依赖注入，ShouldStop，ContextTransform，session 接线 |
| **命令系统** | `bot/command/` | 斜杠命令解析与注册，Callbacks 模式解耦 |
| **配置** | `bot/config/` | `~/.nekocode/config.json` 加载 |
| **TUI** | `tui/` | Bubble Tea v2，BotInterface（18 方法）解耦，组件化 |

## Skill 系统

### 双层 Skill 来源

1. **Bundled Skills** (`bot/skill/bundled/`)：编译进二进制的内置技能，使用 `go:embed` 加载 `meta/SKILL.md`，始终可用
2. **File-based Skills** (`bot/skill/loader.go`)：从 `.nekocode/skills/` 目录自动发现 YAML 技能定义

### 注册顺序

Bundled skills 优先注册，保证内置技能优先级高于文件系统技能。Skill 同时注册为斜杠命令（`/<skill-name>`）和工具（供 Agent 调用）。

### 技能上下文管理

Agent 调用技能时，技能内容注入为 user 消息。下一轮若不再需要该技能，自动清除技能消息，释放上下文空间。

## 配置

`~/.nekocode/config.json`：

```json
{
  "provider": "openai",
  "api_key": "sk-...",
  "model": "gpt-4",
  "base_url": "https://api.openai.com/v1",
  "token_budget": 128000,
  "thinking_budget": -1,
  "max_iterations": 0
}
```

`~/.nekocode/memory.md` — 自动创建的 session memory 文件。
