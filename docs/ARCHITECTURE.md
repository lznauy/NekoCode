# NekoCode 架构文档

> **本文档职责**: 描述项目架构——目录结构、包依赖、模块实现、代码层面的机制。不包含 UI 设计、交互设计、设计原则等属于 DESIGN.md 的内容。

## 项目概述

NekoCode 是一个基于 Go 的终端 AI 助手，使用 Bubble Tea v2 构建 TUI，支持多 LLM provider（OpenAI 兼容 / Anthropic 兼容协议），具备 Agent 循环、Native Function Calling、工具执行、权限确认、Plan Mode、Plugin 系统、事件驱动 Hooks、MCP 客户端、子 Agent、上下文管理、Session Memory、AI 文生图等机制。

## 目录结构

```
nekocode/
├── main.go
├── common/                         # 公共类型
│   ├── types.go                    #   DangerLevel / TodoItem / BotStats / CmdResult
│   ├── confirm.go                  #   ConfirmRequest
│   └── util.go                     #   通用辅助函数
├── llm/                            # LLM 抽象层
│   ├── types/                      #   核心类型定义
│   │   └── types.go                #     LLM 接口 + Message/Response/ToolDef + HTTP 客户端
│   ├── anthropic/                  #   Anthropic 兼容协议
│   │   └── client.go               #     Anthropic Messages API 兼容实现
│   ├── openai/                     #   OpenAI 兼容协议（DeepSeek / MiniMax 等）
│   │   └── client.go               #     OpenAI Chat Completions 兼容实现
│   ├── factory.go                  #   NewClient / NewClientWithProtocol 工厂
│   └── retry.go                    #   指数退避重试
├── bot/                            # 核心逻辑
│   ├── bot.go                      #   Bot 结构体 + 依赖注入
│   ├── api.go                      #   公开 API（BotInterface 实现）
│   ├── plugin_commands.go          #   插件命令注册
│   ├── agent/                      #   Agent 循环
│   │   ├── agent.go                #     Agent 结构体
│   │   ├── run.go                  #     Run() 主循环 + handleText
│   │   ├── run_exec.go             #     executeAndFeedback（工具执行 + PostTool hooks）
│   │   ├── reasoner.go             #     Reason() + withRetry
│   │   ├── budget/                 #     预算与配额
│   │   │   ├── exploration.go      #       探索螺旋检测
│   │   │   └── quota.go            #       每轮工具配额
│   │   └── subagent/               #     子 Agent 系统
│   │       ├── agents.go           #       内置 agent 类型定义（3 种：executor/verify/researcher）
│   │       ├── agent_md.go         #       AgentMD 解析（Claude Code 格式）
│   │       ├── engine.go           #       子 Agent 执行引擎
│   │       ├── registry.go         #       注册表（builtins + plugins）
│   │       ├── result.go           #       结果类型 + 安全审核
│   │       └── prompts/            #       子 Agent prompt 模板
│   │           ├── executor.md      #         executor prompt
│   │           ├── researcher.md    #         researcher prompt
│   │           └── verify.md        #         verify prompt
│   ├── config/                     #   配置管理
│   │   └── config.go               #     Config + Load()
│   ├── command/                    #   斜杠命令系统
│   │   ├── parser.go               #     Parser + Callbacks
│   │   └── lifecycle.go            #     SummarizeIfNeeded / ForceFreshStart / ContextStats
│   ├── ctxmgr/                     #   上下文管理
│   │   ├── manager.go              #     Manager：Build() 入口 + NewSub + 消息管理
│   │   ├── build.go                #     Build 管线（孤儿过滤）
│   │   ├── storage.go              #     消息存取
│   │   ├── report.go               #     上下文诊断报告 + 彩色 bar
│   │   ├── compact/                #     压缩子系统
│   │   │   ├── compact.go          #       FullCompact
│   │   │   ├── compactor.go        #       Compactor 核心
│   │   │   ├── levels.go           #       五级预警阈值
│   │   │   ├── micro.go            #       MicroCompact
│   │   │   ├── budget.go           #       工具结果预算截断
│   │   │   ├── collapse.go         #       Context Collapsing
│   │   │   ├── merge.go            #       Archive 摘要合并
│   │   │   ├── prompt.go           #       压缩 prompt 模板
│   │   │   └── snipe.go            #       冷历史切除
│   │   ├── context/                #     上下文内容定义
│   │   │   └── content.go          #       Content 结构体 + BuildLayer*（含 Memory 字段）
│   │   ├── memory/                 #     Session Memory
│   │   │   └── memory.go           #       Memory 结构体
│   │   └── token/                  #     Token 估算
│   │       ├── estimate.go         #       启发式估算
│   │       └── tracker.go          #       API 校准追踪
│   ├── debug/                      #   全局调试日志（独立子系统，不依赖 compact）
│   │   └── log.go                  #     debug.Log（时间戳 + 来源 + subagent 标签 + 10MB rotate）
│   ├── hooks/                      #   Hook 系统（事件驱动）
│   │   ├── hooks.go                #     HookPoint / Hint / StopReason / Hook / Registry + Evaluate
│   │   ├── keys.go                 #     事件 key 常量（13 个）
│   │   ├── builtin.go              #     RegisterBuiltin（7 个内置 Hook）
│   │   └── plugin.go               #     声明式 hooks（JSON 配置驱动，6 种事件类型）+ LoadPluginHooks
│   ├── mcp/                        #   MCP 客户端
│   │   ├── client.go               #     JSON-RPC 2.0 客户端 + Server 管理
│   │   └── tool.go                 #     MCPTool 适配器
│   ├── plugin/                     #   Plugin 系统
│   │   ├── registry.go             #     Registry（安装/卸载/启用/禁用 + LoadAll）
│   │   ├── manifest.go             #     Manifest 解析（plugin.json）
│   │   └── exec.go                 #     git clone + 文件复制 + install.sh 检测
│   ├── cindex/                     #   代码索引（替代 projctx）
│   │   ├── manager.go              #     入口管理器，协调各组件，处理降级
│   │   ├── graph.go                #     核心数据结构（Node, Edge, Graph）+ 查询接口
│   │   ├── db.go                   #     SQLite schema、持久化、FTS5 搜索
│   │   ├── parser.go               #     Tree-sitter 解析引擎，提取符号和关系
│   │   ├── index.go                #     索引编排（全量扫描、跨文件引用解析）
│   │   ├── sync.go                 #     增量同步（fsnotify 监听 + 防抖）
│   │   ├── traversal.go            #     BFS/DFS 图遍历、路径查找
│   │   ├── tool.go                 #     project_info tool 接口层
│   │   └── project.go              #     NEKOCODE.md 项目上下文发现
│   ├── treesitter/                 #   Tree-sitter 语言支持
│   │   └── langs.go                #     语言注册 + 查询定义
│   ├── prompt/                     #   System Prompt 构建
│   │   ├── builder.go              #     Prompt 构建器
│   │   ├── system.md               #     英文 System Prompt 模板
│   │   └── system_zh.md            #     中文 System Prompt 模板
│   ├── session/                    #   Session 管理
│   │   └── session.go              #     Session 持久化
│   ├── skill/                      #   技能系统
│   │   ├── skill.go                #     Skill 定义
│   │   ├── loader.go               #     YAML 加载 + 目录发现
│   │   ├── tool_skill.go           #     技能工具注册
│   │   └── bundled/                #     内置技能（go:embed）
│   │       ├── bundled.go           #       嵌入入口
│   │       └── meta/                #       技能元数据
│   └── tools/                      #   工具系统
│       ├── types.go                #     Tool 接口 + ToolCallItem + ToolCallResult + Descriptor
│       ├── executor.go             #     Executor + 权限检查 + ExecuteBatch
│       ├── registry.go             #     注册表
│       ├── file_cache.go           #     文件缓存（Seed/Merge/LRU）
│       ├── util.go                 #     辅助函数（HashLine / StripAnsi / ValidatePath）
│       ├── hashline/               #     Hashline 编辑子系统
│       │   ├── hash.go             #       文件内容哈希计算
│       │   ├── patch.go            #       Patch DSL 解析
│       │   ├── apply.go            #       编辑应用 + 边界修复
│       │   ├── recovery.go         #       3-way merge 恢复
│       │   ├── mismatch.go         #       不匹配处理
│       │   └── snapshot.go         #       快照管理
│       └── builtin/                #     内置工具
│           ├── register.go         #       RegisterAll()
│           ├── block_resolver.go   #       Tree-sitter 块解析器（Go/Python/JS/TS/Rust）
│           ├── tool_bash.go        #       Bash 执行
│           ├── tool_read.go        #       文件读取
│           ├── tool_write.go       #       文件写入
│           ├── tool_edit.go        #       文件编辑（hashline 锚点）
│           ├── tool_glob.go        #       文件匹配
│           ├── tool_grep.go        #       内容搜索
│           ├── tool_list.go        #       目录列表
│           ├── tool_task.go        #       子 Agent 任务
│           ├── tool_todo.go        #       Todo 管理
│           ├── tool_webfetch.go    #       Web 抓取
│           ├── tool_websearch.go   #       Web 搜索
│           ├── tool_tree.go         #       目录树
│           ├── tool_image_gen.go    #       图片生成（即梦文生图）
│           └── html2md.go          #       HTML→Markdown
├── tui/                            # TUI 界面
│   ├── tui.go                      #   package tui 入口（Run 函数）
│   ├── agent.go                    #   Agent 桥接 + startChat
│   ├── model.go                    #   Model 结构体
│   ├── update.go                   #   Update() 消息分发
│   ├── view.go                     #   View() 视图布局组装
│   ├── handlers.go                 #   按键处理
│   ├── helpers.go                  #   辅助函数
│   ├── types.go                    #   BotInterface + 消息类型
│   ├── components/                 #   UI 组件
│   │   ├── block/                  #     内容块渲染
│   │   │   ├── block.go             #       Block 结构体 + Done 字段
│   │   │   ├── block_render.go      #       渲染逻辑
│   │   │   └── block_tool.go        #       工具块 + edit 预览渲染
│   │   ├── message/                #     消息项渲染
│   │   │   ├── message.go           #       Message 结构体
│   │   │   ├── message_assistant.go #       助手消息渲染
│   │   │   ├── message_user.go      #       用户消息渲染
│   │   │   ├── message_system.go    #       系统消息渲染
│   │   │   ├── message_error.go     #       错误消息渲染
│   │   │   ├── message_shared.go    #       共享 helper
│   │   │   └── markdown.go          #       Markdown 渲染（段落级分离）
│   │   ├── processing/             #     处理中状态渲染
│   │   │   ├── processing.go        #       Processing 结构体
│   │   │   ├── processing_render.go #       渲染逻辑
│   │   │   └── render_text.go       #       文本渲染
│   │   ├── messages.go             #     消息列表
│   │   ├── input.go                #     输入框
│   │   ├── header.go               #     顶部状态栏
│   │   ├── splash.go               #     启动页
│   │   ├── confirm_bar.go          #     确认栏
│   │   ├── list_widget.go          #     列表组件
│   │   ├── suggestions.go          #     命令补全
│   │   └── scrollbar.go            #     滚动指示器
│   └── styles/                     #   样式
│       ├── colors.go               #     色彩体系
│       └── charset.go              #     制表符字符集
```

## BotInterface（12 方法）

```go
type BotInterface interface {
    RunAgent(input, onStep) (string, error)
    ExecuteCommand(input) (string, CmdResult)
    SkillHint() (string, bool)
    Stats() BotStats
    CommandNames() []string
    Configure(confirmFn, phaseFn, todoFn, notifyFn, confirmCh)
    SetCallbacks(textFn, reasonFn)
    Steer(msg string)
    Abort()
    ProviderModel() (provider, model string)
    SwitchModel(name) (model, provider string, err error)
    SessionMessages() []DisplayMessage
}
```

## 核心架构：Agent 循环

```
用户输入
  │
  ▼
Run() 主循环 → runTurn(state)
  │
  ├─ UserSubmit hooks: Evaluate → [System] hints
  ├─ AutoCompactIfNeeded() 看门狗
  ├─ budget.ComputeQuota() 计算工具配额
  ├─ PreTurn hooks: Evaluate → Layer2 hints
  ├─ drainSteering() 排空中途输入
  │
  ├─ Reason(state) → ReasoningResult
  │   ├─ phase(PhaseThinking)
  │   ├─ ctxMgr.Build(true) 组装上下文（全部消息，不再截断）
  │   ├─ transform(messages) 消息变换钩子
  │   ├─ llmClient.ChatStream() 流式调用
  │   └─ withRetry() 指数退避重试
  │
  ├─ [工具调用] executeAndFeedback(calls, reasoning, state)
  │   ├─ 配额过滤 + PreToolUse hooks（per-tool）
  │   ├─ 工具执行 + 事件记录（Inc/Flag）
  │   ├─ PostToolUse hooks（per-tool）
  │   └─ PostTool hooks（batch）: Evaluate → Stop/Hint
  │
  ├─ [文本响应] handleText(reasoning, state)
  │   ├─ Emit garbled/chat Turn
  │   └─ PostTurn hooks: Evaluate → Stop/Hint
  │
  └─ synthesizeAndReturn() 兜底总结
```

## 上下文管理

### 五级预警阈值

| Level | 剩余 buffer | 动作 |
|-------|------------|------|
| Normal | > 44,800 | 无 |
| Warning | ≤ 44,800 | 告警 |
| MicroCompact | ≤ 35,200 | 微压缩 |
| Compact | ≤ 25,600 | 完整压缩 |
| Blocking | ≤ 6,400 | 拒绝 |

### Build 管线

1. Layer 0: SystemPrompt + Skills（静态前缀）
2. Layer 0: Memory（项目记忆，内容通过 context.Content.Memory 字段承载）
3. Layer 0.5: Archive（压缩摘要）
4. Layer 1: Messages（全部保留，不再截断；Compactor 负责压缩）
5. Layer 2: Todo + Hints（动态层）

## 工具系统

### Tool 接口

```go
type Tool interface {
    Name() string
    Description() string
    Parameters() []Parameter
    ExecutionMode(args map[string]any) ExecutionMode
    DangerLevel(args map[string]any) DangerLevel
    Execute(ctx context.Context, args map[string]any) (string, error)
}
```

### 内置工具

| 工具 | 模式 | 危险等级 |
|------|------|----------|
| bash | Sequential | 智能分级（Safe～Forbidden） |
| read | Parallel | Safe |
| write | Sequential | Write |
| edit | Sequential | Write（hashline 锚点定位） |
| list | Parallel | Safe |
| glob | Parallel | Safe |
| grep | Parallel | Safe |
| web_search | Parallel | Safe |
| web_fetch | Parallel | Safe |
| task | Parallel | Safe |
| todo_write | Sequential | Safe |
| tree | Parallel | Safe |
| project_info | Parallel | Safe（cindex 代码索引：symbol/deps/file/search/skeleton） |
| image_gen | Sequential | Safe（即梦文生图 · 火山引擎 SigV4） |
| skill | Parallel | Safe（技能包加载） |

## Hook 系统（事件驱动）

### 七种触发点

| Point | 时机 | 注入方式 |
|-------|------|---------|
| PreTurn | LLM 推理前 | Layer2 hints |
| PreToolUse | 单个工具执行前（per-tool） | `[System]` 消息 |
| PostToolUse | 单个工具执行后（per-tool） | `[System]` 消息 |
| PostTool | 全部工具执行后（batch） | `[System]` + Stop |
| PostTurn | LLM 纯文本返回后 | `[System]` + Stop |
| UserSubmit | 用户提交输入后 | `[System]` 消息 |
| Stop | Agent 循环结束时 | Stop 判定 |

### 事件存储模型

使用 `Registry` + `Snapshot` 模式：单一 `map[string]int64` 存储所有事件值，`map[string]string` 存储字符串值。通过 key 前缀约定语义（`counter:` 跨轮持久，`turn:` / `gauge:` / `flag:` / `value:` 由 Agent.Reset 或 ResetSession 重置）。

### 内置 Hook（7 个）

| Hook | Point | 功能 |
|------|-------|------|
| quota | PreTurn | 读取配额不足时告警，引导优先实质性修改 |
| verification | PostTurn | 有未完成任务但本轮无工具调用时提醒继续 |
| exploration_exhausted | PreTurn | 探索调用 ≥10 且分数耗尽时强制行动 |
| explore_cascade | PostTool | 本轮启动 ≥4 个 researcher 时提醒综合信息 |
| tool_idle | PostTool | 连续 50 次只读工具调用后警告开始写代码 |
| completion_quality | PostTurn | 任务全标完成但未修改文件时提醒 |
| garbled_circuit_breaker | PostTurn | 累计 5 次 garbled 工具调用则强制停止 |

### 跨轮状态

`counter:` 前缀的 key 跨轮持久（仅 ResetSession 清除），`flag:` / `turn:` / `gauge:` / `value:` 前缀的 key 每轮 ResetTurn 清除。

## Plugin 系统

`bot/plugin/`：
- 安装源：GitHub URL / user:repo / 本地路径
- 扩展点：Skills / Agents / Hooks / MCP Servers
- `/plugin install/list/uninstall/enable/disable/info`

## 声明式 Hooks

`bot/hooks/plugin.go`（`LoadPluginHooks`）：
- 事件类型：PreToolUse / PostToolUse / PostToolUseFailure / UserPromptSubmit / SessionStart / Stop
- JSON 配置（hooks.json）
- Tool name matcher（`|` 分隔，regex 支持）
- 命令执行 + 超时

## MCP 客户端

`bot/mcp/`：
- JSON-RPC 2.0 协议
- Server 生命周期管理（启动/初始化/心跳/tool 列举/关闭）
- `tools.Tool` 接口适配（MCPTool）
- 危险等级可配置

## 子 Agent 系统

### 内置类型（3 种）

| Agent | 用途 | 工具 |
|-------|------|------|
| executor | 执行代码修改 | read/write/edit/bash/grep/glob/list |
| verify | 验证修改 | read/grep/glob/list/bash |
| researcher | 代码探索/调研 | read/grep/glob/list/web_search/web_fetch |

### Engine 特性

- 独立 ctxmgr（NewSub），可选接入 Compactor
- FileCache 从主 Agent 种子预热（Seed/Merge）
- 上下文窗口、Thinking 开关等参数从主 Agent 配置继承
- 安全审核（关键词匹配 + 敏感路径检测）
- DisableThinking 默认关闭，researcher 支持 Thoroughness 深度控制
- Handoff 上下文注入（`<handoff>` 块追加到 system prompt）
- ConfirmFn 覆盖（edit 操作需用户确认）
- Partial result 恢复（中断/错误时返回部分结果）
- Metadata 追踪（totalTokens、toolUseCount、durationMs、cacheHitTokens、cacheMissTokens）
- Phase 回调（cfg.OnPhase 通知阶段变化）

### AgentMD 解析

`bot/agent/subagent/agent_md.go`：解析 Claude Code 格式的 `agents/*.md`（YAML frontmatter）。

## TUI 组件树

```
Model
├── Header         — provider/model · tokens
├── Splash         — 启动页
├── Messages       — 消息列表 + Scrollbar
├── Suggestions    — 命令补全
├── Input          — 消息输入框（3 行固定高度，SetPromptFunc 控制换行）
├── ConfirmBar     — 确认栏（工具 + 插件安装）
└── notifyCh       — 异步通知通道
```

## 模块职责

| 模块 | 位置 | 职责 |
|------|------|------|
| Agent 循环 | `bot/agent/` | Reason→Execute→Feedback，中断，重试 |
| | 子 Agent | `bot/agent/subagent/` | 独立循环，3 种内置类型 + 插件扩展 |
| LLM 网关 | `llm/` | OpenAI/Anthropic 双协议，统一接口 |
| 工具系统 | `bot/tools/` | Tool 接口 + Executor + Registry + FileCache |
| | 内置工具 | `bot/tools/builtin/` | 12 个内置工具实现 + image_gen（条件注册） |
| | Hashline | `bot/tools/hashline/` | 编辑锚点 · 哈希计算 · Patch DSL · 3-way merge |
| SDK | `bot/sdk/` | 外部服务 SDK（火山引擎 SigV4 签名） |
| 上下文管理 | `bot/ctxmgr/` | Build 管线 + 五级压缩 + token 估算 |
| Session Memory | `bot/ctxmgr/memory/` | Memory 文件持久化（10 section Markdown） |
| Plugin 系统 | `bot/plugin/` | 安装/卸载/生命周期 |
| MCP 客户端 | `bot/mcp/` | JSON-RPC 2.0 |
| Hook 系统 | `bot/hooks/` | 事件驱动（7 种触发点）+ 声明式（plugin.go） |
| Tree-sitter | `bot/treesitter/` | 多语言解析器注册 + AST 查询 |
| 命令系统 | `bot/command/` | 斜杠命令解析 |
| 调试日志 | `bot/debug/` | 全局 debug.Log（时间戳 + subagent 标签） |
| TUI | `tui/` | Bubble Tea v2 组件化 |
