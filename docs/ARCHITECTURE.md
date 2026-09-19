# NekoCode 架构文档

> **本文档职责**: 描述项目架构——目录结构、包依赖、模块实现、代码层面的机制。不包含 UI 设计、交互设计、设计原则等属于 DESIGN.md 的内容。

## 项目概述

NekoCode 是一个基于 Go 的终端 AI 助手，使用 Bubble Tea v2 构建 TUI，支持多 LLM provider（OpenAI 兼容 / Anthropic 兼容协议），具备 Agent 循环、Native Function Calling、工具执行、权限确认、Plan Mode、Plugin 系统、事件驱动 Hooks、MCP 客户端、子 Agent、上下文管理、项目 Memory、AI 文生图等机制。

## Bot 层目标结构

Bot 层按子系统组织，目录结构和依赖方向都应接近树状：`bot/core` 负责直接装配，下层负责能力实现；通过明确所有权和单向依赖避免横向穿透与反向依赖，不为绕过包关系额外制造窄接口。

目标分层：

```
bot/
├── core/                # Bot 对外 API + 生命周期组合根
│   ├── bot.go           # Bot 类型与初始化
│   ├── bot_run.go       # RunHost、运行与命令执行
│   ├── bot_services.go  # 模型、配置、上下文、指标领域查询
│   ├── bot_session.go   # 会话领域操作
│   ├── bot_extension.go # 扩展领域操作
│   └── bot_subagent.go  # 子 Agent 接线
├── agent/               # Agent 主循环：turn、LLM 调用、工具反馈、停止条件
├── agent/subagent/      # 子 Agent 执行引擎
├── contextmgr/          # 上下文、压缩、memory、token 统计
├── provider/            # LLM 协议、客户端工厂、stream/http 类型
├── policy/              # 确定性策略：Policy、Hook 引擎（Registry/builtin/plugin）、ledger
├── extension/           # plugin、skill、mcp、tool 能力实现、执行内核与统一管理
├── command/             # slash command 注册和生命周期命令
├── session/             # 会话持久化
├── prompt/              # system/plan prompt 构建
└── config/
```

依赖规则：

- `bot/core` 是装配层，可以直接依赖各子系统；其他子系统不能反向依赖 `bot/core`。
- `bot/**` 不依赖 `runtime` 或展示层；Bot 只公开领域对象和
  `protocol` 中的中立运行契约。
- `runtime/standard` 是完整 Bot 到 runtime 协议的唯一适配边界，view DTO
  的投影集中在其 `internal/viewmodel`。
- `agent` 只依赖 LLM、context、tools、policy 等运行时接口，不关心 plugin/skill/mcp 的安装和发现。
- `tools` 只定义和执行工具，不反向依赖 agent 主循环或 `agent/subagent`；需要委托子 Agent 时通过既有 task runtime 接线，具体适配器放在 `bot/core`。
- `agent/subagent` 不依赖 `agent` 主循环；接收已解析的 `agentprofile.Profile`，不负责插件发现或注册。`RunConfig.Policy` 只接收审计事件，写授权使用每次运行独立创建的 guard。
- `extension.Manager` 是扩展系统唯一的高层入口；`bot/core` 不直接持有 plugin/skill/mcp 子 Manager。
- `plugin` 只管理插件清单、安装和启停状态；扩展激活由 `extension.Manager` 统一编排。
- `mcp.Manager` 同时拥有 server 和对应工具的生命周期，外层不手工同步工具切片。
- Skill 发现、上下文格式化和 loaded 状态归 `extension.Manager`；command 只接收动态命令注册数据。
- 无引用的 alias 包应删除；内部新代码应直接依赖真实实现包。

## 目录结构

### 项目配置的生命周期

`bot/project` 读取固定项目根目录的 `NEKOCODE.md` 和 `.nekocode/.mcp.json`，保存各文件最后一次有效结果及诊断，不修改进程目录，也不授予工具权限。`bot/core` 将项目 MCP 与全局配置按名称合并后交给扩展管理器；全局配置对象保持独立，设置保存不会写入项目定义。扩展管理器在创建时固定 Skills 和插件目录，刷新时更新 MCP、插件和技能，不重启未变更且正常运行的宿主 MCP。

项目规范进入主 Agent 的稳定提示词，并作为已解析的指令传给子 Agent。会话恢复前校验目录归属，恢复后重新读取当前规范，防止历史快照覆盖当前项目指令。`/workspace reload` 和管理界面的刷新共用项目刷新流程，由现有运行时 mutation 边界安排在任务之间执行。

项目刷新按准备和发布两个阶段执行：短暂持有 Bot 状态锁取得配置副本与上一份完整扩展视图，随后在锁外读取项目文件、扫描扩展及处理 MCP 进程；完成后在锁内发布新的项目规范和配置。刷新期间管理视图返回旧快照，命令候选菜单直接提示刷新中，避免等待扩展管理器的锁。独立刷新锁串行化重复刷新请求；命令注册表使用读写锁，并一次性替换全部技能命令，回调在注册表锁外执行。

这里的项目根目录决定配置来源；`bot/extension/tool/runtime/workspace` 继续负责文件访问权限。当前项目根目录就是启动目录，不向上搜索；Shell、索引和 LSP 等执行路径仍保留单项目运行约束，尚未提供同进程多项目执行或运行中切换项目。

```
nekocode/
├── main.go                         # Wails GUI 入口 + interaction/gui/web/dist embed
├── wails.json                      # Wails 构建配置
├── cmd/
│   ├── tui/
│   │   └── main.go                 # TUI 程序入口
│   └── daemon/
│       └── main.go                 # HTTP API daemon 入口
├── interaction/gui/app/            # Wails 后端桥接实现
│   ├── app.go                      #   App 结构体 + 事件推送
│   ├── appicon.icns                #   macOS 应用图标（多分辨率）
│   └── appicon.ico                 #   Windows 应用图标（多分辨率）
├── interaction/gui/web/            #   Wails 前端（React + Vite）
│   ├── index.html
│   ├── src/
│   │   └── components/
│   │       └── LogoMark.tsx        #   Logo 组件（猫娘图标，32×32 viewBox）
│   └── public/
│       └── logo/                   #   品牌资产源文件
│           ├── logo.svg             #     完整品牌版（含 NEKOCODE 字）
│           ├── logo-icon.svg        #     纯图标源
│           ├── logo-icon-app.svg    #     favicon
│           ├── appicon.icns         #     macOS 导出
│           ├── appicon.ico          #     Windows 导出
│           ├── icon_16..1024.png    #     中间产物
│           └── build_icns.js        #     任意平台生成 ICNS 的脚本
├── protocol/                       # Bot/runtime 共用的中立运行契约
├── acp/                            # ACP v1 stdio/JSON-RPC 适配与 runtime 事件投影
├── headless/                       # NekoCode 双向 NDJSON 基础协议，独立适配 runtime
├── logger/                         # 项目诊断日志
├── util/                           # 通用工具包（duration / fs / http / registry / sse / text / url / yaml / version / tui_snapshot）
├── runtime/                        # 交互控制层（TUI/GUI/HTTP/connector 的统一入口）
│   ├── runtime.go                  #   入口：Runtime + New(Runner)
│   ├── runner.go                   #   必需 Runner/RunHost 执行协议
│   ├── runtime_services.go         #   可选能力接口
│   ├── runtime_run.go              #   单次 run host 与事件转换
│   ├── runtime_control.go          #   start/steer/cancel/approval/question
│   ├── runtime_events.go           #   事件、录制与 connector 通道
│   ├── runtime_status.go           #   状态、查询、指标与能力发现
│   ├── protocol.go                 #   稳定 DTO、状态常量与协议错误
│   ├── agentrunner/                #   通用 Agent 到 Runner 的适配
│   ├── standard/                   #   标准应用 runtime 组装
│   │   ├── adapter.go              #   Bot 领域对象 → runtime 协议
│   │   ├── internal/viewmodel/     #   Bot 领域对象 → runtime DTO
│   │   └── standard.go             #   New(): Bot + 事件录制 + connector
│   ├── httpapi/                    #   HTTP/SSE API server
│   └── internal/                   #   内部实现
│       ├── core/                   #     protocol/interaction/views 契约源
│       ├── broker/                 #     approval/question 同步交互
│       ├── eventbus/               #     事件总线
│       ├── runstore/               #     run 状态存储
│       ├── recording/              #     事件录制
│       └── connectors/             #     connector 注册表
├── bot/                            # 核心逻辑
│   ├── core/                       #   组合根与对外 Bot API
│   │   ├── bot.go                  #     Bot + New()
│   │   ├── bot_run.go              #     运行、命令与 RunHost 回调转发
│   │   ├── bot_services.go         #     配置、模型、指标、上下文与 Memory 服务
│   │   ├── bot_extension.go        #     extension 组装、生命周期与领域操作
│   │   ├── bot_session.go          #     session 状态编排与领域操作
│   │   └── bot_subagent.go         #     task tool 到 subagent engine 接线
│   ├── agent/                      #   Agent 循环
│   │   ├── agent.go                #     入口：Agent + New(Config)
│   │   ├── loop.go                 #     Run 主循环
│   │   ├── agent_turn.go           #     单轮策略与结果处理
│   │   ├── model.go                #     LLM 调用、流回调与响应分类
│   │   ├── tools.go                #     工具过滤、执行、反馈与子代理回调
│   │   ├── agent_state.go          #     run、stream 与 token 状态
│   │   ├── slots.go                #     子代理槽位
│   │   ├── internal/kernel/        #     通用循环、生命周期与重试门
│   │   ├── internal/llmstream/     #     LLM 流式调用与工具调用解析
│   │   ├── subagent/               #     子 Agent 系统
│   │   │   ├── subagent.go         #       入口、运行循环与结果
│   │   │   ├── subagent_engine.go  #       上下文、推理与工具执行
│   │   │   ├── config.go           #       已解析 Profile、运行依赖与通用契约
│   │   │   └── prompts/            #       子 Agent prompt 模板
│   │   │       └── subagent.md     #         通用子 Agent prompt
│   ├── checkpoint/                 #   回合级文件快照与 rewind
│   │   ├── checkpoint.go           #     锚点、写前快照、恢复与索引
│   │   └── checkpoint_test.go      #     新增/修改/删除恢复验收
│   ├── config/                     #   配置管理
│   │   └── config.go               #     Config + Load()
│   ├── command/                    #   斜杠命令系统
│   │   ├── command.go              #     Handler 入口
│   │   ├── parser.go               #     命令解析与注册
│   │   └── lifecycle.go            #     ForceCompact / ContextReport
│   ├── contextmgr/                 #   上下文管理
│   │   ├── contextmgr.go           #     入口：Manager + Config + New
│   │   ├── contextmgr_context.go   #     Build 管线、设置与 token 用量
│   │   ├── contextmgr_history.go   #     消息存取与截断
│   │   ├── contextmgr_compaction.go #    压缩控制
│   │   ├── compression.go          #     私有压缩阈值、预算与摘要函数
│   │   ├── replacement.go          #     摘要替换压缩实现
│   │   ├── contextmgr_snapshot.go  #     Snapshot / Restore
│   │   ├── contextmgr_report.go    #     上下文诊断报告
│   │   ├── content.go              #     私有上下文内容与分层构建
│   │   ├── memory/                 #     五段式项目 Memory 只读加载
│   │   └── token/                  #     Token 估算
│   │       ├── estimate.go         #       启发式估算
│   │       └── tracker.go          #       API 校准追踪
│   ├── provider/                   #   LLM 抽象层
│   │   ├── types/                  #     核心类型定义（Message/Response/ToolDef + HTTP 客户端）
│   │   ├── anthropic/              #     Anthropic Messages API 兼容实现
│   │   ├── openai/                 #     OpenAI Chat Completions 兼容实现（DeepSeek / MiniMax 等）
│   │   ├── provider.go             #     入口：LLM 接口、Config 与 New 工厂
│   │   └── retry.go                #     指数退避重试
│   ├── policy/                    #   确定性治理引擎
│   │   ├── policy.go              #     唯一入口：Policy 构造、生命周期、快照
│   │   ├── policy_tools.go        #     BeforeTool / RecordTool(s)
│   │   ├── events.go              #     ToolRequest / ToolResult / Facts
│   │   ├── hook.go                #     Hook / State / Result 对外类型
│   │   ├── hooks.go               #     Hook 注册、私有状态、评估
│   │   ├── audit.go               #     Hook 审计事件与输出格式化
│   │   ├── plugin.go              #     LoadPluginHooks（声明式 hooks）
│   │   ├── builtin/               #     内置 Hook 实现
│   │   │   └── all.go             #       注册、修改前强制读取、乱码响应熔断
│   │   ├── plugin/                #     声明式 Hook 实现
│   │   │   ├── types.go           #       Point / Event / Hint / Result / Hook 类型
│   │   │   ├── config.go          #       配置加载
│   │   │   ├── schema.go          #       JSON Schema
│   │   │   ├── hook.go            #       Hook 执行
│   │   │   ├── matcher.go         #       Tool name matcher
│   │   │   └── runner.go          #       命令执行 + 超时
│   │   ├── ledger/                #     工具执行账本
│   │   │   └── ledger.go          #       确定性事件、路径集合与快照
│   ├── extension/                  #   扩展系统
│   │   ├── extension.go            #     入口：Manager（插件/Skill/Hook/Agent/MCP 生命周期）
│   │   ├── extension_commands.go   #     /plugin 命令与安装交互
│   │   ├── mcp/                    #     MCP 客户端
│   │   │   ├── mcp.go              #       入口：Manager（server 生命周期 + 能力发现 + 健康状态）
│   │   │   ├── client.go           #       client：进程连接句柄（Start/Close/ListTools/CallTool）
│   │   │   ├── types.go            #       ServerConfig 等类型定义
│   │   ├── plugin/                 #     Plugin 系统
│   │   │   ├── plugin.go           #       入口：Plugin 核心类型 + Manager（清单与状态）
│   │   │   ├── plugin_commands.go  #       插件命令展示与安装预览
│   │   │   ├── registry.go         #       registry（存储 + 状态持久化）
│   │   │   ├── install.go          #       安装获取（git clone / 本地复制）
│   │   │   ├── manifest.go         #       Manifest 解析（plugin.json）
│   │   │   ├── source.go           #       源解析 / env 展开 / 远程获取
│   │   │   └── format.go           #       插件展示文本
│   │   ├── skill/                  #     Skill 系统
│   │   │   ├── skill.go            #       入口：Skill 核心类型 + Manager（加载、上下文刷新、skill tool 接线）
│   │   │   ├── registry.go         #       skill registry（包内）
│   │   │   ├── load.go             #       获取链：目录发现 → 读取 → 解析 → 内置技能（go:embed）
│   │   │   ├── format.go           #       格式化
│   │   │   ├── tool.go             #       技能工具适配
│   │   │   └── bundled/            #       内置技能文件
│   │   └── tool/                   #     工具能力与执行内核
│   │       ├── tools.go            #       Registry + 统一注册元数据
│   │       ├── builtin/            #       内置工具实现
│   │       │   ├── capability/     #         MCP constant-schema 代理工具
│   │       │   ├── catalog/        #         Toolbox 工具与生命周期
│   │       │   ├── filesystem/     #         read/write/edit/list/tree/glob/grep
│   │       │   ├── shell/          #         Bash 执行 + 危险分级
│   │       │   ├── web/            #         web_search / web_fetch / web_extract / html2md
│   │       │   ├── media/          #         image_gen（即梦文生图）
│   │       │   ├── task/           #         子 Agent 任务工具
│   │       │   ├── todo/           #         todo_write 工具
│   │       │   ├── question/       #         question 工具
│   │       │   ├── diff/           #         diff 工具
│   │       │   └── index/          #         代码索引工具（条件注册）
│   │       └── runtime/            #       工具执行编排
│   │           ├── core/           #         Tool 接口 + ToolCallItem/Result + Descriptor
│   │           ├── runner/         #         执行引擎（单工具/批量/预览/权限）
│   │           ├── execution/      #         执行状态 + 缓存 + 文件快照
│   │           ├── permission/     #         权限确认
│   │           ├── sandbox/        #         沙箱隔离
│   │           ├── workspace/      #         工作区访问控制
│   │           ├── taskbridge/     #         task 工具到 subagent 的桥接
│   │           └── toolutil/       #         路径/文本等通用工具
│   ├── prompt/                     #   System Prompt 构建
│   │   ├── prompt.go               #     入口：Builder + New
│   │   ├── system_zh.md            #     中文 System Prompt 模板
│   │   ├── env.go                  #     环境信息格式化
│   │   └── os_release.go           #     OS 检测
│   ├── session/                    #   Session 管理
│   │   ├── session.go              #     入口：Manager + New(cwd)
│   │   └── snapshot.go             #     Snapshot / Meta 持久化与 context 映射
├── interaction/connect/            # 外部 IM connector
│   ├── connect.go                  #   入口：connector 基座和事件分发
│   ├── pairing.go/store.go         #   配对状态与 connect.json 分段配置
│   ├── commands.go/stream.go       #   共享指令与流式节流
│   ├── telegram/                   #   Telegram connector（polling + pairing + taskview 渲染）
│   ├── feishu/                     #   飞书 connector（Lark SDK WS 长连接 + DM 配对，MVP）
│   ├── qqbot/                      #   QQ connector（腾讯官方机器人平台：Gateway WS + v2 消息 API，纯文本）
│   └── wecom/                      #   企业微信智能机器人（官方 WebSocket 长连接 + owner 配对）
├── interaction/interaction.go      # 多种交互端共享的工具参数展示格式
├── interaction/tui/                # TUI 交互界面
│   ├── tui.go                      #   package tui 入口（Run 函数）
│   ├── agent.go                    #   runtime 事件到 TUI 消息的投影
│   ├── model.go                    #   Model 结构体
│   ├── update.go                   #   Update() 消息分发
│   ├── view.go                     #   View() 视图布局组装
│   ├── handlers.go                 #   按键处理
│   ├── helpers.go                  #   token 统计文案
│   ├── types.go                    #   状态枚举 + 消息类型
│   ├── components/                 #   UI 组件
│   │   ├── block/                  #     内容块渲染
│   │   │   ├── block.go            #       Block 结构体 + Done 字段
│   │   │   ├── block_render.go     #       渲染逻辑
│   │   │   └── block_tool.go       #       工具块 + edit 预览渲染
│   │   ├── message/                #     消息项渲染
│   │   │   ├── message.go          #       Message 结构体
│   │   │   ├── message_assistant.go#       助手消息渲染
│   │   │   ├── message_user.go     #       用户消息渲染
│   │   │   ├── message_system.go   #       系统消息渲染
│   │   │   ├── message_error.go    #       错误消息渲染
│   │   │   ├── message_shared.go   #       共享 helper
│   │   │   └── markdown.go         #       Markdown 渲染（段落级分离）
│   │   ├── processing/             #     处理中状态渲染
│   │   │   ├── processing.go       #       Processing 结构体
│   │   │   ├── processing_render.go#       渲染逻辑
│   │   │   └── render_text.go      #       文本渲染
│   │   ├── messages.go             #     消息列表
│   │   ├── input.go                #     输入框
│   │   ├── header.go               #     顶部状态栏
│   │   ├── splash.go               #     启动页
│   │   ├── confirm_bar.go          #     确认栏
│   │   ├── list_widget.go          #     列表组件
│   │   ├── suggestions.go          #     一级命令补全 + 动态分级命令菜单
│   │   └── scrollbar.go            #     滚动指示器
│   └── styles/                     #   样式
│   │   ├── colors.go               #     色彩体系
│   │   └── charset.go              #     制表符字符集
```

## Runtime 控制契约

`runtime.New(runner, services)` 只要求 Runner 实现 `Run(context.Context, input, RunHost)`；可选应用能力由组合根通过 `runtime.Services` 显式提供。流输出、工具/子 Agent 事件、phase、todo、approval 和 question 都经本次调用独占的 `RunHost` 传递；命令在一次同步生命周期中返回完整结果。

`Runtime` 是 UI/HTTP/connector 的唯一交互入口，核心方法为 `StartRun`、`SteerRun`、`CancelRun`、`Status`、`LookupRun`、`Events`、`DecideApproval` 和 `AnswerQuestion`。命令、Steering、Metrics、Model、Context、Extension、Configuration 和 Session 是 `runtime.Services` 中的显式函数字段；UI 先读 `Capabilities()`，再直接调用同一个 Runtime 的只读方法。关闭统一使用 `Close() error`，connector、Runner 和 recorder 的错误不会被吞掉。

事件协议版本为 `2.0`，每个事件携带单调 `sequence`；订阅可用 `EventFilter.After` 续传。协议错误使用稳定 `ErrorCode`。标准应用由 `runtime/standard.New()` 装配完整 Bot、事件录制和 connector；`examples/web-assistant` 则展示只组合 web 工具的独立应用。

上层应用、第三套 UI 和自定义 connector 的实现教程见 [RUNTIME_APP_GUIDE.md](RUNTIME_APP_GUIDE.md)。

## Bot 应用层（bot/core）

`bot/core` 是核心依赖注入和生命周期编排层。`Bot` 结构体持有所有子系统引用，通过 `New()` 按顺序初始化：

```
New()
  ├── initConfig()        → config.Load() + prompt.New()
  ├── initCtxMgr()        → contextmgr.New() + index.Apply()
  ├── initSession()       → session.New(cwd) + checkpoint.New()
  └── rebuildRuntime()
      ├── initToolRegistry()     → catalog.NewToolbox() + 条件工具
      ├── initPolicy()           → policy.New() + builtin.Register()
      ├── initExtensions()       → extension.New(Config).Load() + config MCP
      ├── initAgent()            → provider.New(Config) + agent.New(Config)
      ├── ctxMgr.SetCompactionModel() → 配置摘要与归档合并模型
      └── initCommands()         → command.RegisterAll() + session/export/plugin 命令
```

## 核心架构：Agent 循环

`bot/agent/agent.go` 是 L2 Agent 核心入口，公开构造使用 `agent.New(ctx, agent.Config{...})`。

```
用户输入
  │
  ▼
Run() 主循环 → runTurn(state)
  │
  ├─ AutoCompactIfNeeded() 看门狗
  ├─ drainSteering() 排空中途输入
  │
  ├─ Reason(state) → ReasoningResult
  │   ├─ phase(PhaseThinking)
  │   ├─ Policy.BeforeModel() → tagged request-scoped hints
  │   ├─ ctxMgr.BuildRequest() 追加变化的 runtime user context 并组装请求
  │   ├─ callLLMForTool() 流式调用
  │   └─ withRetry() 指数退避重试
  │
  ├─ [工具调用] executeAndFeedback(calls, reasoning, state)
  │   ├─ Policy.BeforeTool()：确定性 PreToolUse 决策
  │   ├─ 工具执行
  │   └─ Policy.RecordTools()：账本 + PostToolUse/PostToolBatch
  │
  ├─ [文本响应] handleText(reasoning, state)
  │   ├─ Emit garbled/chat Turn
  │   └─ Policy.BeforeStop() → 接受、阻止或要求修正最终回答
  │
  └─ synthesizeAndReturn() 兜底总结
```

Agent 循环硬限制：
- `maxAgentSteps = 150`：最大迭代步数
- `maxConsecutiveHints = 3`：连续纯文本提示上限
- `maxConsecutiveFailures = 5`：连续 LLM 调用失败上限
- `maxFinalCheckHints = 2`：最终检查重试上限

### Agent 相关包结构

| 路径 | 职责 |
|------|------|
| `bot/agent/agent.go` | Agent 入口、配置与生命周期 |
| `bot/agent/internal/kernel/` | Agent 循环核心控制流（Loop/RunLoop、Lifecycle、Gate），零业务依赖 |
| `bot/agent/internal/llmstream/` | LLM 流式调用 + 工具调用解析 + 重试 |
| `bot/agent/subagent/` | 子 Agent 引擎 + 运行契约 + 工具权限检查 |
| [`bot/policy/`](../bot/policy/README.md) | Policy：组合确定性 hook 与 ledger 的唯一入口；治理原则与非目标见目录 README |
| `bot/policy/ledger/` | 工具执行账本：readFiles / modifiedFiles / blockedTools / toolErrors |

## 上下文管理

### 自动压缩阈值

上下文占用达到 `auto_compact_percent` 时执行一次全量摘要替换，默认值为 80%。阈值按当前
模型上下文窗口计算，不再维护没有独立行为的 Warning、MicroCompact、Compact、Blocking
状态。摘要完成后若上下文仍达到模型窗口上限，返回真实的 context full 错误。

### Build 管线

1. Layer 0: SystemPrompt + Skills（会话内静态前缀）
2. Layer 1: Memory（项目记忆）
3. Layer 2: Archive（压缩摘要）
4. Layer 3: Messages（当前缓存 epoch 内严格只追加）
5. Runtime Context：Environment、Todo、Runtime Policy 按固定顺序组成
   `<runtime_context mode="replace">` 完整快照，内容变化时作为隐藏的 `role=user` 消息追加到
   Layer 3；空字段带显式状态，最新快照覆盖旧快照语义
6. Hints：turn hint 与 BeforeModel hint 各自格式化为 `<runtime_policy_hints>`，仅在存在时
   作为独立的隐藏 `role=user` 消息追加；hint 消息不重复携带其他 runtime 状态，清空 hint
   也不会追加空快照。每条 hint 明确只作用于紧邻的下一次模型响应，因此历史重放不会让
   已消费的 hint 重新生效
7. Runtime Events：checkpoint rewind 等已经发生且需要纠正模型认知的事件，作为独立隐藏
   `role=user` 消息永久追加；事件进入 Session 和后续摘要，但不在聊天界面显示

请求发出前，`contextmgr` 对稳定段生成 prefix shape：system（Layer 0-1）、
tools 和 history（Layer 2-3）。动态变量不再以临时 system tail 重建：新快照进入历史后，
下一次请求会原样重放。旧消息被改写、删除或压缩才归因为 history 变化；压缩、新建空白
会话和模型切换视为新的缓存 epoch；`rewind` 不删除对话历史，而是在文件恢复成功后追加隐藏的
`<workspace_event type="checkpoint_rewind">`，列出每个文件的回滚动作和受影响父目录，因此仍保持
append-only。压缩时 runtime 快照不进入 Archive，活动历史只保留
最新一份完整快照。**恢复会话不是缓存 epoch**：Layer 0-3 全部按会话文件重放，其中 Layer 0
（system prompt + skills）必须是 registry 与 context window 的纯函数，不得携带"哪些 skill
已加载"之类的会话状态，否则恢复会改写前缀开头、令供应商重读整段历史；已加载状态只用于
skill 工具语义与会话持久化。恢复时同时写回上次请求的 head 摘要（system/tools），使重载后的
首次 miss 归因为 `system`/`tools` 或 `tail/provider`，而不是 `cold-start`。累计缓存命中/
未命中属会话级诊断，随会话恢复；provider 校准的 prompt token 不恢复，因为它依赖当前模型。
上一轮的轮统计（调用数、命中/未命中、Biggest miss 及归因）也随会话保存，只保留一个槽位：本次
进程跑完第一轮之前 `/context` 的 `Last turn` 呈现它，之后由本进程的轮取代，因此重载不会让
Cache 块只剩累计一行。`Last turn` 下先给最低命中率的一次，最后给 `Biggest miss`（未缓存最多
的一次，附原因）。调用数来自每次 `BuildRequest` 观察，而命中/未命中只在供应商上报缓存
明细时累加，所以"有调用数、无 token"表示这批调用未上报缓存，报告显示 `cache usage not
reported` 而不是把它记成 0% 命中。归档的累计计数（压缩次数、被裁剪消息数）同样随会话保存并
恢复，否则重载后会出现"有归档却 0 次压缩"；`/context` 的 `Archive` 行据此显示归档规模与压缩
次数，无归档时显示 `none (no compaction yet)`。Provider 上报 cache miss 后，最近一次
system/tools/history 变化会记录到 ContextReport、逐调用 calllog，并由 `/context` 展示。

`llm-calls.jsonl` 不保存 prompt、HTTP 请求正文或供应商错误正文，只记录每次调用的
session/model/source、脱敏 endpoint、延迟、system fingerprint 短哈希、prefix 诊断和
`total tok · in · cached · new · out` usage；`total = in + out`。`session` 取调用发生时
的当前会话 id，主模型、subagent 与 compaction 记录由各 Bot 实例显式携带，可据此归组同一次会话的
全部调用。供应商上报
reasoning token 明细且值大于 0 时，尾部追加 `reasoning`；它属于 out 的子集，不重复计入
total。供应商未上报缓存明细时，cached/new 显示为 `?`，
不会误记为零命中。Agent 同时按当前 run 汇总同口径 usage；Runtime 在 `RunDone` 前发布
最终 metrics，TUI 将其显示为 assistant 回合尾注：使用“输入 / 缓存 / 未缓存 / 输出 / 推理”
等中文语义标签，并在末尾显示本次命中率；窄终端切换为仍可辨识的中文短标签。主模型、
subagent 和 compaction 请求都进入同一统计口径；command-only run 不产生 turn telemetry。

Provider 层先用 `StreamUsage.Merge` 合并协议碎片，再通过单一 `OnUsage` 事件向上传递完整调用；
context cache tracker、run meter 和 calllog 不直接消费 SSE 分片。TUI 直接渲染中立协议层的
`Metrics`，不再维护第二套 telemetry DTO。模型能力注册表先将配置解析为 `ReasoningSettings`，
命令菜单、配置校验和 provider 共享这一结果：未知模型只有 Auto，Off 仅对明确支持关闭推理的
模型开放。Auto 不发送覆盖字段；`none` 与 compaction/subagent 的显式 disable 只有在能力表
定义了原生关闭方式时才映射为禁用，否则内部调用移除已配置 effort 并回退到模型默认行为。
各 provider 仅负责翻译解析后的 wire 设置。calllog 同时记录 requested/effective effort，便于
区分用户配置、实际生效值与内部调用覆盖。

模型相关默认值集中在 `config.modelProfile`：同一条 profile 同时定义上下文窗口和 reasoning
能力，型号匹配只针对去除 provider 前缀后的 model ID，避免独立 substring 表产生能力漂移。

推理原文与 provider 请求采用分离投影：session 始终保存模型返回的 reasoning，供 TUI、恢复和
审计使用；`ReasoningSettings.Replay` 决定 wire 是否回放。DeepSeek 只在 assistant 工具调用消息
上回放 `reasoning_content`，Anthropic 只回放带 provider signature 的 thinking block，普通历史
推理不会进入后续请求；隐藏正文但保留 signature 的 thinking block 仍按原 wire 形状回放。
ContextReport 与自动压缩门限使用同一 replay contract 估算实际输入，
切换模型时清除旧 provider 的 prompt 校准基线并按新模型重新计算。

### Runtime 关键方法

| 方法 | 说明 |
|------|------|
| `Build()` | 组装完整消息列表（含孤儿过滤） |
| `BuildRequest()` | 原子组装模型请求并记录 system/tools/history 稳定形状 |
| `New(Config)` | 使用同一配置协议创建主 Agent 或子 Agent 上下文 |
| `AutoCompactIfNeeded(ctx)` | 自动压缩看门狗 |
| `Summarize(ctx)` | 手动触发完整压缩 + Archive 合并 |
| `Snapshot() / Restore()` | 会话持久化 |
| `Reset()` | 清空当前上下文，用于创建不继承旧内容的新会话 |

### Session 持久化实现

保存链路（`bot/core/bot_run.go` `runAgent`）：

```
ag.Run()  →  ctxMgr.SetSystemPrompt()  →  ctxMgr.Summarize()（按需）  →  saveSession()
```

- `bot.saveSession()` 显式收集 Context、token、Skill 和 ledger，再交给
  `session.Manager.Save(snapshot)`。`session.json` 只保存可继续推理的活动上下文；完整的用户、助手和
  工具消息按序追加到 `transcript.jsonl`，压缩不会改写它。恢复后的 UI 和导出读取 transcript，LLM
  只读取活动上下文。每次自动或手动压缩前都会先保存当前快照，并在 `backups/` 中创建不可覆盖的
  `pre-compact-*.session.json`；备份失败时不调用摘要模型。
  Session 包不通过回调访问 Agent、Extension 或 Policy。
- `Snapshot()`（`bot/contextmgr/contextmgr_snapshot.go`）**深拷贝** `Messages` 后再返回，避免后续 `append` 共享 backing array 导致已捕获内容被覆盖
- 触发时机：每次 agent run 结束自动保存；`/sessions`、`/export`、`/new` 等命令也会触发

Agent 结束时的文本处理（`bot/agent/loop.go` `finishRun`）：

- `finalText` 优先，为空时回退到 `lastText`
- 仅在真正无文本产出时才调 `Synthesize()`（追加的总结消息是最后手段）
- `applyFinalPolicyBlock`（`bot/agent/agent_turn.go`）会同步设 `finalText = lastText`，避免无谓的 `Synthesize` 追加

> **历史 bug**: `Snapshot()` 曾只拷贝 slice 头，叠加 `applyFinalPolicyBlock` 只设 `lastText` 不设 `finalText`，导致 `finishRun` 误走 `Synthesize` 路径，保存了一条用户没见过的假摘要覆盖真实最后内容。已通过深拷贝 + `finalText`/`lastText` 同步修复。

### Tool 接口

```go
type Tool interface {
    Name() string
    Description() string
    Parameters() []Parameter
    ExecutionMode(args map[string]any) ExecutionMode
    Execute(ctx context.Context, args map[string]any) (string, error)
}
```

### 工具注册

`bot/extension/tool/builtin/catalog/toolbox.go` 中的 `Toolbox` 注册内置工具（shell/process/read/write/list/tree/glob/edit/grep/web_search/web_fetch/web_extract/question/todo_write/task/diff/index）。`image_gen` 按配置条件注册；Extension manager 统一注册 `skill` 和 constant-schema `capability` 工具。`bot/core/bot.go` 只负责 Extension、Agent 和 command parser 的顶层组装。

`Registry` 除了保存 `Tool`，还集中保存 preview 与 delegated-call target 等执行元数据。模型侧名称仍是固定的 `capability`，但解析后的 canonical identity 会随 `ToolCallItem` 贯穿权限、Pre/Post Hook、Ledger、audit、结果和 UI 回调。canonical identity 会转义 server/tool 名称中的 `%` 和 `__`，避免不同 server.tool 组合碰撞到同一条权限规则。Runner 和 Policy 都不通过 optional interface 或硬编码 MCP 参数来推断行为。

### 内置工具

| 工具 | 模式 | 危险等级 | 位置 |
|------|------|----------|------|
| shell | Sequential | 智能分级（Safe～Forbidden），多层沙箱隔离 | `bot/extension/tool/builtin/shell/` |
| process | Sequential | Safe，管理既有托管进程 | `bot/extension/tool/builtin/shell/` |
| read | Parallel | Safe | `bot/extension/tool/builtin/filesystem/read/` |
| write | Sequential | Write | `bot/extension/tool/builtin/filesystem/write/` |
| edit | Sequential | Write（oldString/newString 内容锚定 + gofmt lint） | `bot/extension/tool/builtin/filesystem/edit/` |
| list | Parallel | Safe | `bot/extension/tool/builtin/filesystem/list/` |
| glob | Parallel | Safe | `bot/extension/tool/builtin/filesystem/search/` |
| grep | Parallel | Safe | `bot/extension/tool/builtin/filesystem/search/` |
| web_search | Parallel | Safe | `bot/extension/tool/builtin/web/` |
| web_fetch | Parallel | Safe | `bot/extension/tool/builtin/web/` |
| web_extract | Parallel | Safe | `bot/extension/tool/builtin/web/` |
| question | Sequential | Safe | `bot/extension/tool/builtin/question/` |
| diff | Parallel | Safe | `bot/extension/tool/builtin/diff/` |
| task | Parallel | Safe | `bot/extension/tool/builtin/task/` |
| todo_write | Sequential | Safe | `bot/extension/tool/builtin/todo/` |
| tree | Parallel | Safe | `bot/extension/tool/builtin/filesystem/tree/` |
| index | Parallel | Safe（条件注册） | `bot/extension/tool/builtin/index/` |
| image_gen | Sequential | Safe（条件注册） | `bot/extension/tool/builtin/media/` |
| skill | Parallel | Safe（动态注册） | `bot/extension/skill/` |
| capability | Sequential | 按真实 MCP `mcp__server__tool` 目标执行权限匹配 | `bot/extension/tool/builtin/capability/` |

### 工具系统子包

| 子包 | 职责 |
|------|------|
| `builtin/catalog/` | Toolbox 工具组装与生命周期 |
| `builtin/filesystem/{read,write,edit,list,tree,search}/` | 文件系统工具 |
| `builtin/shell/` | Shell 执行、托管进程、事件式等待 + 危险分级 |
| `builtin/web/` | Web 搜索/直抓/正文抽取/HTML2MD |
| `builtin/media/` | 图片生成（即梦文生图） |
| `builtin/task/` | 子 Agent 任务工具 |
| `builtin/todo/` | Todo 管理工具 |
| `builtin/question/` | 用户提问工具 |
| `builtin/diff/` | Diff 工具 |
| `builtin/index/` | 代码索引工具（条件注册） |
| `builtin/capability/` | MCP constant-schema 能力发现、检查与调用代理 |
| `runtime/core/` | Tool 接口 + ToolCallItem/Result + Descriptor |
| `runtime/runner/` | 工具执行引擎（单工具/批量/预览/权限） |
| `runtime/execution/` | 执行状态 + 缓存 + 文件快照 |
| `runtime/permission/` | 权限确认 |
| `runtime/sandbox/` | 沙箱隔离 |
| `runtime/workspace/` | 工作区访问控制 |
| `agent/internal/llmstream/` | LLM 流式调用 + 工具调用解析 |
| `runtime/taskbridge/` | task 工具到 subagent 的桥接 |
| `runtime/toolutil/` | 参数、路径与文本等无状态工具函数 |

## Hook 系统（事件驱动）

### 六种触发点

| Point | 时机 | 典型作用 |
|-------|------|---------|
| UserSubmit | 用户输入进入本次运行后 | 校验输入、注入运行级提示 |
| PreModel | 每次模型请求前 | 注入仅对本次请求生效的系统提示 |
| PreToolUse | 单个工具执行前 | 允许或阻止该工具调用 |
| PostToolUse | 单个工具执行后 | 根据单次结果追加提示或停止 |
| PostToolBatch | 一批工具全部执行后 | 根据整批活动和进度作出决策 |
| Stop | 模型准备返回最终文本时 | 接受、阻止或要求修正最终回答 |

### 事件与状态

Runtime 只向 `Policy` 提交 `ToolRequest`、`ToolResult` 和 `TurnResult`。
Policy 只生成工具调用结果、目标文件是否存在/已读、乱码计数等可直接观测的
`Facts`。Hook 的计数器和去重标记按 hook 名隔离，不能读写其他规则或 runtime
的内部字段。

### 默认内置 Hook（2 个）

默认规则只处理可机械确认的条件，不根据调用次数、命令名称、todo 或探索状态
推断任务是否完成、验证是否充分或模型下一步应该做什么。

| Hook | Point | 功能 |
|------|-------|------|
| read_before_write | PreToolUse | 主 Agent 的 edit/write 前检查当前 run 读取记录 |
| garbled_circuit_breaker | Stop | 累计 5 次 garbled 工具调用则强制停止 |

## Plugin 系统

`bot/extension/`：
- 唯一高层入口是 `Manager`（`extension.go`）：`New(Config{Context, Tools, Policy, ContextWindow})` 后调用 `Load()`
- 统一装配 builtin tools、Skills、Agents、Hooks、MCP Servers 与模型侧 capability 代理
- 插件安装、启停和 Reload 都经过同一条生命周期；Skill 每次只按当前启用插件集合重载一次
- 通过 `Snapshot()` 一次返回管理视图状态，并提供 Skill 查询、插件启停等精简子方法
- `tool/` 是 Extension 领域内的稳定调用 ABI 与执行内核；具体能力实现依赖它，Agent 不需要理解 MCP、Skill 或 Plugin 的内部协议

`bot/extension/plugin/`：
- 入口是 `Manager`（`plugin.go`）：只拥有 registry、安装管线和启停状态
- 安装源：GitHub URL / user:repo / 本地路径
- 扩展点：Skills / Agents / Hooks / MCP Servers
- manifest / registry / 安装源模型
- 插件自身不注册 Agent、Hook、MCP 或 Skill，不持有宿主回调
- 不依赖 Runtime DTO：GUI/API 管理快照由 `runtime/standard` 投影

## 声明式 Hooks

`bot/policy/plugin/`（`LoadPluginHooks`）：
- 事件类型：PreToolUse / PostToolUse / PostToolUseFailure / UserPromptSubmit / SessionStart / Stop（6 种）
- JSON 配置（hooks.json）
- Tool name matcher（`|` 分隔，regex 支持）
- 命令执行 + 超时
- 支持 `Once` 标记（仅首次触发）

## MCP 客户端

`bot/extension/mcp/`：
- 唯一入口是 `Manager`（`mcp.go`）：`New()`；`Add`/`Remove`/`Close` 管理 server 生命周期与动态能力清单
- JSON-RPC 2.0 协议
- Server 生命周期管理（启动/初始化/tool 列举/关闭）
- MCP 工具不再逐个注册给 provider；`extension/tool/builtin/capability` 直接绑定 Manager，通过固定 schema 的单一工具完成 list / inspect / call
- capability 的真实目标由 Registry 注册元数据解析为 `mcp__server__tool`，并作为 effective identity 贯穿权限与治理链路；Runner 不感知 MCP 参数格式
- stable owner ID 与展示名称分离；config MCP 优先，同名插件 MCP 会被拒绝

## Skill 系统

`bot/extension/skill/`：
- 唯一入口是 `Manager`（`skill.go`）：`New(contextManager, toolRegistry, contextWindow)`；registry 与 tool 适配器均为包内实现
- YAML 格式技能定义
- 目录发现 + 加载
- 内置技能通过 `bundled/` go:embed
- `skill` 工具动态注册到 toolRegistry
- `Load(pluginDirs)` / `Reload(pluginDirs)` 显式接收插件 Skill 目录，不持有目录回调
- 不依赖 Runtime DTO：GUI/API 管理快照由 `runtime/standard` 使用
  `Manager.Snapshot()` 投影
- 核心 system prompt 只保留全局授权、安全、信任与证据边界；诊断、设计和交付审查流程按需从 Skill 注入，避免每轮重复携带

### 内置技能

| Skill | 触发场景 | 工作流 |
|-------|---------|--------|
| `hunt` | 报错、失败测试、异常行为、根因排查 | 复现 → 可证伪假设 → 交叉证据 → 根因/修复 |
| `think` | 功能构思、架构、方案取舍、实施计划 | 现状调研 → 推荐方案 → 前提与风险 → 用户批准 |
| `check` | diff/PR/issue 审查、非平凡实现后的交付检查 | 范围核对 → 风险审查 → 独立复核 → 验证 |
| `skill-creator` | 创建、审查或更新技能 | 收集需求 → 编写 SKILL.md → 校验 |

Skill frontmatter 中的 `context`、`agent`、`allowed-tools`、`max_steps` 和 `context_window` 会被解析并保存在 `Skill`；主 Agent 仍可按需 inline 加载正文。委托任务通过 `task(profile, skills, prompt)` 显式选择 profile 和 task-scoped skills，skill 只提供工作流，不能扩展 profile 的工具权限。

## 子 Agent 系统

### 内置 Profile（2 种）

| Profile | 权限边界 | 工具 |
|---------|----------|------|
| coder | 工作区读写与命令执行 | read/write/edit/shell/process/grep/glob/list/web_search/web_fetch/web_extract |
| explore | 严格只读，无任意命令执行 | read/grep/glob/list/web_search/web_fetch/web_extract |

验证、调研、诊断和设计不是 Agent 类型，由 `check`、`learn`、`hunt`、`think` 等 skill 与具体 task prompt 组合表达。插件 AgentMD 由 `extension/agentprofile` 解析；Profile 集合由每个 `extension.Manager` 的 activePlugin 持有，没有进程级可变注册表。

### Engine 特性

- 独立 ctxmgr（`New(Config)`），配置 merge model 时自动接入 Compactor
- FileCache 从主 Agent 种子预热（Seed/Merge）
- 模型和上下文窗口从主 Agent 配置继承；子 Agent 显式关闭 thinking
- profile 工具白名单同时用于模型工具过滤与执行前复核；代理工具需要同时列出代理名与允许的 effective target，不能借代理扩大权限
- 选定 skill 内容在每次模型请求时以 user 级 task-scoped workflow 重投影，不进入可压缩历史，也不授予额外工具
- 每次运行独立的确定性 Policy guard；不以关键词匹配代替权限检查
- Handoff 作为未验证证据留在 user task；选定的 skill workflow 保持 user 权限层级，并在自动压缩后继续逐轮重投影
- ConfirmFn 覆盖（edit 操作需用户确认）
- Partial result 恢复（中断/错误时返回部分结果）
- Metadata 追踪（totalTokens、toolUseCount、durationMs、cacheHitTokens、cacheMissTokens）
- Phase 回调（cfg.OnPhase 通知阶段变化）
- 子 Agent 并发通过 `agent` 的 `slotManager` 管理（最大 8 并发 + 颜色分配）

### AgentMD 解析

`bot/extension/agentprofile/` 解析 AgentMD 的 `name/description/base/tools/skills/max_steps`，归一化工具别名并验证工具、基线和上限。未知字段明确报错。读取通过 `os.Root` 限定在插件内，文件上限 1 MiB。

`extension.Manager` 在锁内先解析整个插件的 Agent 集合，再发布；失败会记录诊断并阻止本次插件运行时激活，Skills 也只从成功激活的插件加载。加载、禁用、重载、安装回滚均由现有插件生命周期管理，无额外注册表服务。对外使用 `plugin/name`，唯一短名称可作别名，内置名称优先。传给 Engine 的 Profile 拷贝包含独立切片，插件关闭不会改变已开始任务的 Profile。

`agent_profiles` 是扩展层提供的固定 schema 只读工具，返回名称、用途、工具上限、默认 Skills、步数及加载错误，不发布系统提示词。`task` 保持固定 schema；插件变化无需改写工具枚举。管理快照经 viewmodel 投影为 GUI 可调用 Agent ID 和错误；CLI `/plugin info` 展示相同状态。

Engine 始终注入通用契约，再追加自定义 Profile 指令；默认 Skills 与 task Skills 在 `bot/core` 合并去重并按 user 权限逐轮投影。Profile 的 `max_steps` 只可收紧默认 50 步，完成协议与禁止嵌套 task 的执行门禁保持独立。

## 治理系统

### Ledger（工具执行账本）

`bot/policy/ledger/ledger.go` 是账本入口，追踪所有工具执行事件，记录：
- `readFiles`：当前执行上下文内专用 `read` 成功读取，或 `write` 成功写入的文件集合
- `modifiedFiles`：专用 `write/edit` 工具成功修改的文件集合
- `blockedTools`：被阻止的工具调用
- `toolErrors`：工具执行错误

Ledger 不解析 shell 命令来推断读取、修改或验证；命令文本只能证明命令被执行，
不能证明其语义或充分性。读取证据不跨 run 恢复；并发 Agent 之间也不共享授权证据。
这条门禁证明“当前执行上下文调用过专用 read”，不声称对外部并发修改提供原子版本保证。

`PreToolUse` 生命周期由主 Agent 与可写子 Agent 执行。每个执行者使用独立授权 ledger，
任何 Agent 的 read 都不能授权另一个 Agent 修改；子 Agent 执行结果另行写入主 run 的共享审计 ledger，
但共享记录不会生成主 Agent 的读取授权。主 policy 的 hooks 不在子 Agent 上复用，避免主 actor
授权状态与子 actor 本地门禁相互污染；子 Agent 只执行每次 run 新建的本地确定性 policy。
失败或被阻断调用不会生成授权证据。

### ResponseGate（响应门控）

`bot/agent/internal/kernel/gate.go`：防止治理内部信号泄漏到模型可见输出。默认最多 2 次重试。

## TUI 组件树

```
Model
├── Header         — provider/model · tokens
├── Splash         — 启动页
├── Messages       — 消息列表 + Scrollbar
├── Suggestions    — 命令补全 + 分级选择（model/checkpoint/session/plugin/connector）
├── Input          — 消息输入框（3 行固定高度，SetPromptFunc 控制换行）
├── ConfirmBar     — 确认栏（工具 + 插件安装）
├── QuestionBar    — 多选/文本问题栏
└── runtimeEvents  — runtime 事件订阅
```

命令菜单沿用命令注册链路：命令所属模块向 `command.Parser` 注册命令说明与动态有限候选，`protocol.CommandMenu` 经 standard adapter/runtime 投影给所有交互端。查询 `/` 或 `$` 得到根菜单，查询完整命令得到下一级菜单；`CommandCatalog` 仅由根菜单派生，用于旧 HTTP 客户端兼容，不再保存第二份命令列表。

TUI 和 GUI 直接渲染菜单；Telegram 渲染 inline keyboard 并同步平台原生命令列表，飞书渲染交互卡片，QQ 等文本渠道渲染编号选择。`interaction/connect.CommandMenus` 只保存有界、短期的回调 ID 映射，完整命令不会进入外部平台回调。所有叶子选择最终仍作为普通命令提交给 Runtime，不绕过命令解析、权限、session 或 checkpoint。自由文本参数不会强制菜单化；runtime 自有的 connect/disconnect 菜单根据 connector 状态动态生成。

## 模块职责

| 模块 | 位置 | 职责 |
|------|------|------|
| Runtime 控制层 | `runtime/` | 交互层统一入口：Runtime + 核心 run 协议 + 可选能力 + HTTP API |
| 中立交互契约 | `protocol/` | Bot 与 runtime 适配器共享的步骤、待办、确认、提问和指标类型 |
| Runtime DTO | `runtime/protocol.go` | runtime 对交互层公开的配置、上下文、会话和扩展数据 |
| 展示格式 | `interaction/interaction.go` | TUI、Connector 等交互端共享的工具摘要 |
| Bot 底座 | `bot/core/` | Agent 能力装配、领域操作与生命周期，不感知 UI/runtime |
| 标准适配 | `runtime/standard/` | Bot 领域协议 → runtime 能力与 UI DTO |
| Agent 循环 | `bot/agent/` | Reason→Execute→Feedback，中断，重试 |
| 治理系统 | `bot/policy/` | Policy：确定性 hook + ledger |
| 工具账本 | `bot/policy/ledger/` | 专用工具事实追踪（读/写/阻止/错误） |
| 推理格式 | `bot/agent/model.go` | LLM 响应分类 + GarbledToolCall 检测 |
| 工具策略 | `bot/policy/` | 确定性工具前后决策、事件收集、hook 注入 |
| 子 Agent | `bot/agent/subagent/` | 通用独立循环，coder/explore profile + task-scoped skills + 插件扩展 |
| 子槽位 | `bot/agent/slots.go` | 并发控制（8 槽位 + 颜色） |
| LLM 网关 | `bot/provider/` | OpenAI/Anthropic 双协议，统一接口 |
| 工具系统 | `bot/extension/tool/` | Registry + builtin 实现 + runtime 执行编排 |
| 工具注册 | `bot/extension/tool/builtin/catalog/` | Toolbox 内置工具组装与关闭 |
| 工具执行 | `bot/extension/tool/runtime/runner/` | 执行引擎（单工具/批量/预览/权限） |
| 文件系统工具 | `bot/extension/tool/builtin/filesystem/` | read/write/edit/list/tree/glob/grep |
| Shell/Process 工具 | `bot/extension/tool/builtin/shell/` | Shell 执行、托管进程与风险分级 |
| Web 工具 | `bot/extension/tool/builtin/web/` | web_search/web_fetch/web_extract/html2md |

`web_fetch` 只做直抓：本地校验目标 URL（拒绝私网/回环/非 http(s)）→ `fetchPageDirect` → `html2md`，
**不接触任何第三方**。它的客户端刻意忽略代理环境变量，并用加固 DialContext 逐 IP 校验、固定目标地址。

`web_extract` 才用托管抽取：对用户 URL 做同样的本地校验，再把包含 scheme 的完整 URL 追加到 `defuddleBaseURL`
（默认 `defuddle.md`，12s 超时、1MB 上限）取回正文 Markdown；非 200、空正文、返回 HTML 或传输错误都
回退到与 `web_fetch` 相同的自抓链路。抽取跳的目的地是常量 host（用户 URL 只作为已校验的 path），因此
允许走环境代理（`HTTPS_PROXY`/`http_proxy` 等），加固 DialContext 仅对用户配置的那个代理地址放行；
回退那一跳与 `web_fetch` 一致：忽略代理、拒绝私网。是否把 URL 交给第三方由 Agent 选择工具决定，
不再有全局开关。
| 媒体工具 | `bot/extension/tool/builtin/media/` | image_gen（即梦文生图） |
| 任务工具 | `bot/extension/tool/builtin/task/`, `bot/extension/tool/builtin/todo/` | sub-agent task 与 todo_write |
| 代码索引工具 | `bot/extension/tool/builtin/index/` | 代码索引（条件注册） |
| 上下文管理 | `bot/contextmgr/` | Build 管线 + 可配置摘要压缩 + token 估算 |
| Project Memory | `bot/contextmgr/memory/` | 项目 Memory 文件加载与 prompt 注入 |
| Plugin 系统 | `bot/extension/plugin/` | manager 编排 + manifest/registry |
| MCP 客户端 | `bot/extension/mcp/` | JSON-RPC 2.0 |
| Skill 系统 | `bot/extension/skill/` | manager + 管理快照 + YAML 技能加载 |
| Hook 系统 | `bot/policy/` | 事件驱动（6 种触发点）+ 声明式（plugin/） |
| 内置 Hook | `bot/policy/builtin/` | 修改前强制读取 + 乱码熔断 |
| 声明式 Hook | `bot/policy/plugin/` | JSON 配置驱动 Hook |
| 命令系统 | `bot/command/` | 斜杠命令解析 |
| 文件回滚 | `bot/checkpoint/` | 用户消息锚点、write/edit 写前快照、最近 100 个消息恢复点与 `/rewind` |
| 诊断日志 | `logger/` | 项目文件日志（时间戳 + subagent 标签） |
| Session 持久化 | `bot/session/` | Manager、Snapshot 与 JSON 存取 |
| TUI | `interaction/tui/` | Bubble Tea v2 组件化 |
| Connector | `interaction/connect/` | Telegram 等外部 IM 接入 |
