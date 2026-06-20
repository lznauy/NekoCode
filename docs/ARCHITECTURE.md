# NekoCode 架构文档

> **本文档职责**: 描述项目架构——目录结构、包依赖、模块实现、代码层面的机制。不包含 UI 设计、交互设计、设计原则等属于 DESIGN.md 的内容。

## 项目概述

NekoCode 是一个基于 Go 的终端 AI 助手，使用 Bubble Tea v2 构建 TUI，支持多 LLM provider（OpenAI 兼容 / Anthropic 兼容协议），具备 Agent 循环、Native Function Calling、工具执行、权限确认、Plan Mode、Plugin 系统、事件驱动 Hooks、MCP 客户端、子 Agent、上下文管理、Session Memory、AI 文生图等机制。

## 目录结构

```
nekocode/
├── cmd/
│   └── main.go                     # 程序入口
├── common/                         # 公共类型
│   ├── types.go                    #   DangerLevel / TodoItem / BotStats / CmdResult / SubSlot
│   ├── confirm.go                  #   ConfirmRequest / ConfirmFunc / PhaseFunc / TodoFunc
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
│   ├── bot.go                      #   package bot 入口（类型别名导出）
│   ├── app/                        #   Bot 应用层（依赖注入 + 生命周期编排）
│   │   ├── bot.go                  #     Bot 结构体 + New() 初始化编排
│   │   ├── api.go                  #     BotInterface 实现（Steer/Abort/ProviderModel/CommandNames/ExecuteCommand/SkillHint）
│   │   ├── api_run.go              #     RunAgent / Configure / SetCallbacks
│   │   ├── api_model.go            #     SwitchModel
│   │   ├── api_stats.go            #     Stats
│   │   ├── api_session_messages.go #     SessionMessages
│   │   ├── init_agent.go           #     Agent 初始化 + wireTaskTool
│   │   ├── init_context.go         #     Config / CtxMgr / Summarizer 初始化
│   │   ├── init_tools.go           #     ToolRegistry + Hooks 初始化
│   │   ├── init_extensions.go      #     Skills / Plugins / MCP 初始化
│   │   ├── init_commands.go        #     Session / Commands 初始化
│   │   ├── session_persistence.go  #     saveSession / ResumeSession
│   │   ├── plugin_commands.go      #     /plugin 命令注册
│   │   ├── plugin_install.go       #     pluginInstall / pluginInstallLocal / pluginInstallAsync
│   │   ├── plugin_manage.go        #     pluginUninstall / pluginEnable / pluginDisable / pluginInfo
│   │   ├── plugin_confirm.go       #     插件安装确认流程
│   │   ├── apistate/               #     API 状态辅助
│   │   │   ├── command.go          #       CommandResult 判定
│   │   │   └── stats.go            #       FormatDuration
│   │   ├── contextguard/           #     上下文守卫
│   │   │   └── tool_results.go     #       ApplyToolResultGuardrail
│   │   ├── contextinit/            #     上下文初始化
│   │   │   └── project.go          #       ApplyProjectContextAndIndex
│   │   ├── pluginops/              #     插件操作
│   │   │   ├── install.go          #       ParseInstallArgs / FetchRemotePreview
│   │   │   └── manage.go           #       RequirePlugin / 格式化输出
│   │   ├── pluginruntime/          #     插件运行时
│   │   │   └── runtime.go          #       Load / Unload（Hooks/Tools/MCP 注册）
│   │   ├── sessioncmd/             #     Session 命令
│   │   │   ├── export.go           #       ExportMessages
│   │   │   └── session_list.go     #       FormatSessionList / ResumeSuccess/Failed
│   │   ├── sessionstate/           #     Session 状态
│   │   │   └── snapshot.go         #       ApplyContextSnapshot / ManagerSnapshot
│   │   └── taskwire/               #     子 Agent 任务接线
│   │       ├── config.go           #       BuildRunConfig
│   │       └── result.go           #       结果处理
│   ├── agent/                      #   Agent 循环
│   │   ├── agent.go                #     package agent 入口（类型别名 → runtime）
│   │   ├── runtime/                #     Agent 运行时核心
│   │   │   ├── agent.go            #       Agent 结构体 + New()
│   │   │   ├── run.go              #       Run() 主循环 + runTurn
│   │   │   ├── run_context.go      #       injectHint / applyTurnHints / synthesizeAndReturn / drainSteering
│   │   │   ├── run_exec.go         #       executeAndFeedback（工具执行 + PostTool hooks）
│   │   │   ├── run_exec_filter.go  #       工具调用过滤（配额 + PreToolUse hooks）
│   │   │   ├── run_exec_posttool.go#       PostToolUse / PostTool hooks 评估
│   │   │   ├── run_exec_results.go #       工具结果合并
│   │   │   ├── run_exec_subagent.go#       子 Agent 回调准备
│   │   │   ├── run_text.go         #       handleText（纯文本响应处理）
│   │   │   ├── run_postturn.go     #       PostTurn hooks 评估
│   │   │   ├── run_preedit.go      #       编辑前预处理
│   │   │   ├── run_final.go        #       finalCheck / evaluateStop
│   │   │   ├── reasoner.go         #       Reason() + ReasoningResult
│   │   │   ├── retry.go            #       callLLMForTool + withRetry
│   │   │   ├── synthesize.go       #       forceSynthesize 兜底总结
│   │   │   ├── gate_alias.go       #       ResponseGate 别名
│   │   │   ├── gov_alias.go        #       GovManager 别名
│   │   │   ├── reasoning_alias.go  #       IsGarbledToolCall 别名
│   │   │   └── subslot_alias.go    #       SubSlotManager 别名
│   │   ├── gate/                   #     响应门控
│   │   │   └── gate.go             #       ResponseGate（治理重试限制）
│   │   ├── governance/             #     治理管理器
│   │   │   ├── gov.go              #       Manager（HookReg + Ledger + Exploration + Gate）
│   │   │   ├── gov_final.go        #       最终检查
│   │   │   ├── gov_lifecycle.go    #       生命周期（Reset/ResetTurn/ResetSession）
│   │   │   ├── gov_observability.go#       可观测性
│   │   │   └── gov_record.go       #       事件记录
│   │   ├── ledger/                 #     工具执行账本
│   │   │   ├── ledger.go           #       Ledger（readFiles/modifiedFiles/blockedTools/verifications）
│   │   │   └── finalcheck.go       #       最终检查逻辑
│   │   ├── reasoning/              #     推理格式检测
│   │   │   └── format.go           #       IsGarbledToolCall
│   │   ├── budget/                 #     预算与配额
│   │   │   ├── exploration.go      #       探索螺旋检测
│   │   │   └── quota.go            #       每轮工具配额
│   │   ├── subagent/               #     子 Agent 系统
│   │   │   ├── agents.go           #       内置 agent 类型定义（3 种：executor/verify/researcher）
│   │   │   ├── agent_md.go         #       AgentMD 解析（Claude Code 格式）
│   │   │   ├── engine.go           #       子 Agent 执行引擎入口
│   │   │   ├── engine_context.go   #       上下文构建
│   │   │   ├── engine_executor.go  #       工具执行
│   │   │   ├── engine_reason.go    #       推理循环
│   │   │   ├── engine_state.go     #       状态管理
│   │   │   ├── registry.go         #       注册表（builtins + plugins）
│   │   │   ├── result.go           #       结果类型
│   │   │   ├── result_builders.go  #       结果构建器
│   │   │   ├── safety.go           #       安全审核
│   │   │   └── prompts/            #       子 Agent prompt 模板
│   │   │       ├── executor.md     #         executor prompt
│   │   │       ├── researcher.md   #         researcher prompt
│   │   │       └── verify.md       #         verify prompt
│   │   └── subslot/                #     子 Agent 并发槽位
│   │       └── manager.go          #       Manager（8 槽位 + 颜色分配）
│   ├── config/                     #   配置管理
│   │   └── config.go               #     Config + Load()
│   ├── command/                    #   斜杠命令系统
│   │   ├── parser.go               #     Parser + Callbacks
│   │   └── lifecycle.go            #     SummarizeIfNeeded / ForceFreshStart / ContextStats
│   ├── contextmgr/                 #   上下文管理
│   │   ├── manager.go              #     Manager：Build() 入口 + NewSub + MakeSummarizer
│   │   ├── build.go                #     Build 管线（孤儿过滤）
│   │   ├── storage.go              #     消息存取（Add/AddAssistantResponse/AddAssistantToolCall/AddToolResultsBatch）
│   │   ├── history.go              #     历史操作（TruncateTo/RemoveMessages/FreshStart）
│   │   ├── compaction.go           #     AutoCompactIfNeeded / Summarize / CompactStats
│   │   ├── settings.go             #     SetSystemPrompt / SetSkillList / SetHints / SetTodos
│   │   ├── snapshot.go             #     Snapshot / Restore（会话持久化）
│   │   ├── stats.go                #     Len / Stats / visibleMessages
│   │   ├── token_usage.go          #     RecordUsage / RecordCache / ResetCache / TokenUsage
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
│   │   │   ├── memory.go           #       Memory 结构体
│   │   │   ├── load.go             #       加载
│   │   │   ├── save.go             #       保存
│   │   │   ├── parse.go            #       解析
│   │   │   ├── build.go            #       构建
│   │   │   ├── merge.go            #       合并
│   │   │   ├── append.go           #       追加
│   │   │   └── fields.go           #       字段定义
│   │   └── token/                  #     Token 估算
│   │       ├── estimate.go         #       启发式估算
│   │       └── tracker.go          #       API 校准追踪
│   ├── debug/                      #   全局调试日志（独立子系统）
│   │   ├── logger.go               #     debug.Log（时间戳 + 来源 + subagent 标签 + 10MB rotate）
│   │   ├── file.go                 #     文件管理
│   │   └── format.go               #     格式化
│   ├── hooks/                      #   Hook 系统（事件驱动）
│   │   ├── events.go               #     HookPoint / Hint / StopReason 定义（7 种触发点）
│   │   ├── keys.go                 #     事件 key 常量（22 个：counter:/turn:/gauge:/flag:/value:/session:/policy:）
│   │   ├── registry.go             #     Registry + Evaluate
│   │   ├── state.go                #     Snapshot 状态存储
│   │   ├── format.go               #     FormatHints
│   │   ├── builtin_register.go     #     RegisterBuiltin（8 个内置 Hook）
│   │   ├── plugin.go               #     LoadPluginHooks（声明式 hooks）
│   │   ├── builtin/                #     内置 Hook 实现
│   │   │   ├── all.go              #       All() 列表
│   │   │   ├── keys.go             #       内置 key 常量
│   │   │   ├── types.go            #       内置 Hook 类型定义
│   │   │   ├── quota.go            #       QuotaHook
│   │   │   ├── verification.go     #       VerificationHook
│   │   │   ├── exploration.go      #       ExplorationExhaustedHook + ExplorationGuardHook
│   │   │   ├── progress.go         #       ExploreCascadeHook + ProgressStallHook
│   │   │   └── quality.go          #       CompletionQualityHook + GarbledCircuitBreaker
│   │   └── plugin/                 #     声明式 Hook 实现
│   │       ├── types.go            #       Point / Event / Hint / Result / Hook 类型
│   │       ├── config.go           #       配置加载
│   │       ├── schema.go           #       JSON Schema
│   │       ├── hook.go             #       Hook 执行
│   │       ├── matcher.go          #       Tool name matcher
│   │       └── runner.go           #       命令执行 + 超时
│   ├── extension/                  #   扩展系统
│   │   ├── mcp/                    #     MCP 客户端
│   │   │   ├── protocol.go         #       JSON-RPC 2.0 协议
│   │   │   ├── process.go          #       Server 进程管理
│   │   │   ├── types.go            #       类型定义
│   │   │   ├── tools.go            #       工具列举
│   │   │   └── tool.go             #       MCPTool 适配器
│   │   ├── plugin/                 #     Plugin 系统
│   │   │   ├── registry.go         #       Registry（安装/卸载/启用/禁用 + LoadAll）
│   │   │   ├── manifest.go         #       Manifest 解析（plugin.json）
│   │   │   ├── model.go            #       Plugin 数据模型
│   │   │   ├── state.go            #       状态管理
│   │   │   ├── discovery.go        #       插件发现
│   │   │   ├── install.go          #       安装逻辑
│   │   │   └── exec.go             #       git clone + 文件复制 + install.sh 检测
│   │   └── skill/                  #     Skill 系统
│   │       ├── registry.go         #       Registry
│   │       ├── model.go            #       Skill 数据模型
│   │       ├── load.go             #       YAML 加载
│   │       ├── parse.go            #       解析
│   │       ├── discovery.go        #       目录发现
│   │       ├── format.go           #       格式化
│   │       ├── tool_skill.go       #       技能工具注册
│   │       └── bundled/            #       内置技能（go:embed）
│   ├── index/                      #   代码索引
│   │   ├── manager.go              #     入口管理器
│   │   ├── tool.go                 #     project_info tool 接口层
│   │   ├── sync.go                 #     同步入口
│   │   ├── db_alias.go             #     DB 别名
│   │   ├── graph_alias.go          #     Graph 别名
│   │   ├── indexer_alias.go        #     Indexer 别名
│   │   ├── parser_alias.go         #     Parser 别名
│   │   ├── db/                     #     SQLite 持久化
│   │   │   ├── db.go               #       DB 连接
│   │   │   ├── schema.go           #       Schema
│   │   │   ├── nodes.go            #       节点操作
│   │   │   ├── files.go            #       文件操作
│   │   │   ├── graph_load.go       #       图加载
│   │   │   └── search.go           #       FTS5 搜索
│   │   ├── graph/                  #     图数据结构
│   │   │   ├── graph.go            #       Node / Edge / Graph
│   │   │   ├── types.go            #       类型定义
│   │   │   ├── lookup.go           #       查询接口
│   │   │   ├── mutate.go           #       变更操作
│   │   │   ├── language.go         #       语言支持
│   │   │   └── format.go           #       格式化
│   │   ├── indexer/                #     索引编排
│   │   │   ├── indexer.go          #       全量扫描
│   │   │   ├── walk.go             #       目录遍历
│   │   │   ├── references.go       #       跨文件引用解析
│   │   │   ├── search.go           #       搜索
│   │   │   ├── file_update.go      #       文件更新
│   │   │   ├── policy.go           #       策略
│   │   │   └── stale.go            #       过期检测
│   │   ├── parser/                 #     Tree-sitter 解析
│   │   │   └── parser.go           #       解析引擎
│   │   ├── projectctx/             #     项目上下文
│   │   │   └── project.go          #       NEKOCODE.md 发现
│   │   ├── projecttool/            #     project_info 工具
│   │   │   └── tool.go             #       NewProjectInfoTool
│   │   ├── service/                #     服务层
│   │   │   └── manager.go          #       Manager
│   │   └── syncer/                 #     增量同步
│   │       └── syncer.go           #       fsnotify 监听 + 防抖
│   ├── treesitter/                 #   Tree-sitter 语言支持
│   │   └── langs.go                #     语言注册 + 查询定义
│   ├── prompt/                     #   System Prompt 构建
│   │   ├── builder.go              #     Prompt 构建器
│   │   ├── system.md               #     英文 System Prompt 模板
│   │   ├── system/                 #     System Prompt 子模块
│   │   │   ├── builder.go          #       构建器
│   │   │   ├── system_zh.md        #       中文 System Prompt 模板
│   │   │   ├── analysis_rules_zh.md#       分析规则
│   │   │   └── os_release.go       #       OS 检测
│   │   └── planmode/               #     Plan Mode
│   │       └── prompt.go           #       Plan Mode prompt
│   ├── session/                    #   Session 管理
│   │   └── session.go              #     Session 持久化
│   ├── sessionview/                #   Session 视图
│   │   └── messages.go             #     DisplayMessages
│   ├── plugincli/                  #   Plugin CLI 辅助
│   │   ├── source.go               #     源解析
│   │   ├── fetch.go                #     远程获取
│   │   ├── format.go               #     格式化输出
│   │   └── env.go                  #     环境变量
│   ├── governance/                 #   工具语义分类
│   │   └── semantics.go            #     Semantics（SourceProducing/Mutating/Verifying）
│   ├── sdk/                        #   外部服务 SDK
│   │   ├── volcengine_signer.go    #     火山引擎签名入口
│   │   └── volcengine/             #     火山引擎 SigV4
│   │       ├── signer.go           #       签名器
│   │       ├── crypto.go           #       加密
│   │       └── canonical.go        #       规范化
│   └── tools/                      #   工具系统
│       ├── types.go                #     Tool 接口 + ToolCallItem + ToolCallResult + Descriptor
│       ├── executor.go             #     Executor + 权限检查 + ExecuteBatch
│       ├── registry.go             #     注册表
│       ├── file_cache.go           #     文件缓存（Seed/Merge/LRU）
│       ├── util.go                 #     辅助函数（HashLine / StripAnsi / ValidatePath）
│       ├── streaming.go            #     流式输出
│       ├── task.go                 #     TaskRunnerTool 接口
│       ├── catalog/                #     工具注册清单
│       │   └── register.go         #       RegisterAll()
│       ├── core/                   #     核心类型
│       │   ├── types.go            #       Tool 接口定义
│       │   └── format.go           #       格式化
│       ├── runner/                 #     工具执行器
│       │   ├── executor.go         #       执行引擎
│       │   ├── execute_one.go      #       单工具执行
│       │   ├── batch.go            #       批量执行
│       │   ├── output.go           #       输出处理
│       │   ├── paths.go            #       路径处理
│       │   └── preview.go          #       预览
│       ├── execution/              #     执行状态
│       │   ├── state.go            #       状态管理
│       │   ├── cache.go            #       缓存
│       │   ├── cache_transfer.go   #       缓存传输
│       │   └── ranges.go           #       范围管理
│       ├── editdsl/                #     Hashline 编辑子系统
│       │   ├── types.go            #       类型定义
│       │   ├── hash.go             #       文件内容哈希计算
│       │   ├── patch.go            #       Patch DSL 解析
│       │   ├── parse_payload.go    #       负载解析
│       │   ├── parse_range.go      #       范围解析
│       │   ├── parse_errors.go     #       错误处理
│       │   ├── apply.go            #       编辑应用
│       │   ├── apply_blocks.go     #       块编辑
│       │   ├── apply_blanks.go     #       空行处理
│       │   ├── apply_boundary.go   #       边界修复
│       │   ├── apply_delimiters.go #       分隔符处理
│       │   ├── apply_landing.go    #       着陆逻辑
│       │   ├── apply_types.go      #       类型
│       │   ├── paths.go            #       路径
│       │   ├── mismatch.go         #       不匹配处理
│       │   ├── recovery.go         #       3-way merge 恢复
│       │   └── snapshot.go         #       快照管理
│       ├── filesystem/             #   文件系统工具
│       │   ├── read/               #     tool_read
│       │   ├── write/              #     tool_write
│       │   ├── edit/               #     tool_edit（hashline 锚点 + block_resolver）
│       │   ├── list/               #     tool_list
│       │   ├── tree/               #     tool_tree
│       │   └── search/             #     tool_glob + tool_grep
│       ├── shell/                  #   Shell 工具
│       │   ├── tool_bash.go        #     Bash 执行
│       │   ├── runner.go           #     运行器
│       │   ├── danger.go           #     危险等级分级
│       │   └── redirection.go      #     重定向
│       ├── web/                    #   Web 工具
│       │   ├── tool_websearch.go   #     Web 搜索
│       │   ├── tool_webfetch.go    #     Web 抓取
│       │   └── html2md.go          #     HTML→Markdown
│       ├── media/                  #   媒体工具
│       │   ├── tool_image_gen.go   #     图片生成（即梦文生图）
│       │   ├── jimeng.go           #     即梦 API
│       │   ├── image_model.go      #     模型配置
│       │   └── image_artifacts.go  #     产物管理
│       ├── tasktool/               #   子 Agent 任务工具
│       │   └── tool_task.go        #     task 工具
│       ├── todo/                   #   Todo 工具
│       │   └── tool_todo.go        #     todo_write 工具
│       ├── llmstream/              #   LLM 流式处理
│       │   ├── call.go             #     调用
│       │   ├── consume.go          #     消费
│       │   ├── tool_calls.go       #     工具调用解析
│       │   └── types.go            #     类型
│       ├── netclient/              #   HTTP 客户端
│       │   └── http.go             #     HTTP 请求
│       ├── pathutil/               #   路径工具
│       │   └── path.go             #     路径验证
│       ├── semantics/              #   工具语义
│       │   └── exploratory.go      #     探索性检测
│       ├── snapshots/              #   快照
│       │   └── snapshots.go        #     快照管理
│       ├── textutil/               #   文本工具
│       │   └── text.go             #     文本处理
│       └── toolhelpers/            #   工具辅助
│           └── helpers.go          #     辅助函数
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
│   │   ├── suggestions.go          #     命令补全
│   │   └── scrollbar.go            #     滚动指示器
│   ├── styles/                     #   样式
│   │   ├── colors.go               #     色彩体系
│   │   └── charset.go              #     制表符字符集
│   └── tui_snapshot/               #   TUI 快照测试
│       └── main.go                 #     快照入口
```

## BotInterface（12 方法）

```go
type BotInterface interface {
    RunAgent(input string, onStep func(action, toolName, toolArgs, output string)) (string, error)
    ExecuteCommand(input string) (string, common.CmdResult)
    SkillHint() (string, bool)
    Stats() common.BotStats
    CommandNames() []string
    Configure(confirmFn common.ConfirmFunc, phaseFn common.PhaseFunc, todoFn common.TodoFunc, notifyFn func(string), confirmCh chan common.ConfirmRequest)
    SetCallbacks(textFn, reasonFn func(string))
    Steer(msg string)
    Abort()
    ProviderModel() (provider, model string)
    SwitchModel(name string) (model, provider string, err error)
    SessionMessages() []common.DisplayMessage
}
```

定义在 `tui/types.go`，由 `bot/app/` 中的 `Bot` 结构体实现。

## Bot 应用层（bot/app/）

`bot/app/` 是核心依赖注入和生命周期编排层。`Bot` 结构体持有所有子系统引用，通过 `New()` 按顺序初始化：

```
New()
  ├── initConfig()        → config.Load() + prompt.NewBuilder()
  ├── initCtxMgr()        → contextmgr.New() + contextinit.ApplyProjectContextAndIndex()
  ├── initToolRegistry()  → catalog.RegisterAll() + projecttool（条件注册）+ editdsl.InitBlockResolver()
  ├── initHooks()         → hooks.RegisterBuiltin()
  ├── initPlugins()       → plugin.NewRegistry().LoadAll() → loadPluginExtensions()
  ├── initSkills()        → skill.NewRegistry() + bundled + Load()
  ├── initSession()       → session.New() + command.NewParser()
  ├── initAgent()         → llm.NewClientWithProtocol() + agent.New() + wireTaskTool()
  ├── initSummarizer()    → ctxMgr.CM.Summarizer = MakeSummarizer()
  └── initCommands()      → command.RegisterAll() + session/export/plugin 命令
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
  │   ├─ callLLMForTool() 流式调用
  │   └─ withRetry() 指数退避重试
  │
  ├─ [工具调用] executeAndFeedback(calls, reasoning, state)
  │   ├─ filterToolCalls() 配额过滤 + PreToolUse hooks（per-tool）
  │   ├─ 工具执行 + 事件记录（Ledger.Inc/Flag）
  │   ├─ PostToolUse hooks（per-tool）
  │   └─ PostTool hooks（batch）: Evaluate → Stop/Hint
  │
  ├─ [文本响应] handleText(reasoning, state)
  │   ├─ Emit garbled/chat Turn
  │   └─ PostTurn hooks: Evaluate → Stop/Hint
  │
  └─ synthesizeAndReturn() 兜底总结
```

Agent 循环硬限制：
- `maxAgentSteps = 150`：最大迭代步数
- `maxConsecutiveHints = 3`：连续纯文本提示上限
- `maxConsecutiveFailures = 5`：连续 LLM 调用失败上限
- `maxFinalCheckHints = 2`：最终检查重试上限

### Agent 子包结构

`bot/agent/agent.go` 是类型别名入口，所有实现位于子包：

| 子包 | 职责 |
|------|------|
| `runtime/` | Agent 结构体 + Run() 主循环 + Reason + 工具执行 + 文本处理 |
| `gate/` | ResponseGate：治理重试限制（默认 2 次） |
| `governance/` | GovManager：整合 HookReg + Ledger + Exploration + Gate |
| `ledger/` | 工具执行账本：readFiles / modifiedFiles / blockedTools / verifications |
| `reasoning/` | IsGarbledToolCall：检测 LLM 输出中的 XML 泄漏 |
| `budget/` | ExplorationTracker + ToolQuota |
| `subagent/` | 子 Agent 引擎 + 注册表 + 安全审核 |
| `subslot/` | 子 Agent 并发槽位管理（8 槽位 + 颜色分配） |

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

### Manager 关键方法

| 方法 | 说明 |
|------|------|
| `Build(withTools)` | 组装完整消息列表（含孤儿过滤） |
| `NewSub(prompt, window, mergeClient)` | 创建子 Agent 轻量 Manager |
| `AutoCompactIfNeeded()` | 自动压缩看门狗 |
| `Summarize()` | 手动触发完整压缩 + Archive 合并 |
| `Snapshot() / Restore()` | 会话持久化 |
| `FreshStart()` | 清空所有消息 |

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

### 工具注册

`bot/tools/catalog/register.go` 中的 `RegisterAll()` 注册所有内置工具。`project_info` 在 `bot/app/init_tools.go` 中条件注册（需要 indexMgr 可用），`image_gen` 在 `RegisterAll` 中条件注册（需要 imageGenModels 非空），`skill` 在 `bot/app/init_extensions.go` 中动态注册。

### 内置工具

| 工具 | 模式 | 危险等级 | 位置 |
|------|------|----------|------|
| bash | Sequential | 智能分级（Safe～Forbidden） | `tools/shell/` |
| read | Parallel | Safe | `tools/filesystem/read/` |
| write | Sequential | Write | `tools/filesystem/write/` |
| edit | Sequential | Write（hashline 锚点定位） | `tools/filesystem/edit/` |
| list | Parallel | Safe | `tools/filesystem/list/` |
| glob | Parallel | Safe | `tools/filesystem/search/` |
| grep | Parallel | Safe | `tools/filesystem/search/` |
| web_search | Parallel | Safe | `tools/web/` |
| web_fetch | Parallel | Safe | `tools/web/` |
| task | Parallel | Safe | `tools/tasktool/` |
| todo_write | Sequential | Safe | `tools/todo/` |
| tree | Parallel | Safe | `tools/filesystem/tree/` |
| project_info | Parallel | Safe（条件注册） | `bot/index/projecttool/` |
| image_gen | Sequential | Safe（条件注册） | `tools/media/` |
| skill | Parallel | Safe（动态注册） | `bot/extension/skill/` |

### 工具系统子包

| 子包 | 职责 |
|------|------|
| `catalog/` | RegisterAll() 注册清单 |
| `core/` | Tool 接口 + 格式化 |
| `runner/` | 工具执行引擎（单工具/批量/预览） |
| `execution/` | 执行状态 + 缓存传输 |
| `editdsl/` | Hashline 编辑 DSL（哈希/解析/应用/恢复/快照） |
| `filesystem/{read,write,edit,list,tree,search}/` | 文件系统工具 |
| `shell/` | Bash 执行 + 危险分级 |
| `web/` | Web 搜索/抓取/HTML2MD |
| `media/` | 图片生成（即梦文生图） |
| `tasktool/` | 子 Agent 任务工具 |
| `todo/` | Todo 管理工具 |
| `llmstream/` | LLM 流式调用 + 工具调用解析 |
| `netclient/` | HTTP 客户端 |
| `pathutil/` | 路径验证 |
| `semantics/` | 探索性检测 |
| `snapshots/` | 快照管理 |
| `textutil/` | 文本处理 |
| `toolhelpers/` | 辅助函数 |

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

使用 `Registry` + `Snapshot` 模式：单一 `map[string]int64` 存储所有事件值，`map[string]string` 存储字符串值。通过 key 前缀约定语义：

| 前缀 | 生命周期 |
|------|---------|
| `counter:` | 跨轮持久（仅 ResetSession 清除） |
| `turn:` | 每轮 ResetTurn 清除 |
| `gauge:` | 每轮 ResetTurn 清除 |
| `flag:` | 每轮 ResetTurn 清除 |
| `value:` | 每轮 ResetTurn 清除 |
| `session:` | 会话级 |
| `policy:` | 策略级 |

### 事件 Key 常量（22 个）

```go
StoreToolPrefix      = "counter:tool:"       // + name
StoreToolResearcher  = "turn:researcher"
StoreQuotaReads      = "gauge:quota_reads"
StoreExploreScore    = "gauge:explore"
StoreTasksAllDone    = "gauge:tasks_done"
StoreHasTasks        = "turn:has_tasks"
StoreTurnToolCalls   = "turn:tool_calls"
StoreStepInputLen    = "turn:step_len"
StoreStepInput       = "value:step"
StoreExploreCalls    = "counter:explore_calls"
StoreHasEdits        = "turn:has_edits"
StoreRespGarbled     = "counter:garbled"
StoreLedgerModified  = "gauge:ledger_modified"
StoreLedgerVerified  = "gauge:ledger_verified"
StoreLedgerErrors    = "gauge:ledger_errors"
StoreLedgerBlocked   = "gauge:ledger_blocked"
StoreLedgerProgress  = "turn:ledger_progress"
StoreSessionStarted  = "session:started"
CounterQuotaWarned   = "counter:quota_warned"
CounterVerifyInjected= "counter:verify_injected"
CounterExploreInjected="counter:explore_injected"
CounterStallTurns    = "counter:stall_turns"
CounterQualityWarned = "counter:quality_warned"
PolicyExploreExhausted="policy:explore_exhausted"
```

### 内置 Hook（8 个）

| Hook | Point | 功能 |
|------|-------|------|
| quota | PreTurn | 读取配额不足时告警，引导优先实质性修改 |
| verification | PostTurn | 有未完成任务但本轮无工具调用时提醒继续 |
| exploration_exhausted | PreTurn | 探索调用 ≥10 且分数耗尽时强制行动 |
| exploration_guard | PreTurn | 探索守卫（新增） |
| explore_cascade | PostTool | 本轮启动 ≥4 个 researcher 时提醒综合信息 |
| progress_stall | PostTool | 连续 50 次只读工具调用后警告开始写代码（原 tool_idle） |
| completion_quality | PostTurn | 任务全标完成但未修改文件时提醒 |
| garbled_circuit_breaker | PostTurn | 累计 5 次 garbled 工具调用则强制停止 |

## Plugin 系统

`bot/extension/plugin/`：
- 安装源：GitHub URL / user:repo / 本地路径
- 扩展点：Skills / Agents / Hooks / MCP Servers
- `/plugin install/list/uninstall/enable/disable/info`
- 插件运行时加载通过 `bot/app/pluginruntime/` 实现

## 声明式 Hooks

`bot/hooks/plugin/`（`LoadPluginHooks`）：
- 事件类型：PreTurn / PreToolUse / PostToolUse / UserSubmit / Stop（5 种）
- JSON 配置（hooks.json）
- Tool name matcher（`|` 分隔，regex 支持）
- 命令执行 + 超时
- 支持 `Once` 标记（仅首次触发）

## MCP 客户端

`bot/extension/mcp/`：
- JSON-RPC 2.0 协议
- Server 生命周期管理（启动/初始化/心跳/tool 列举/关闭）
- `tools.Tool` 接口适配（MCPTool）
- 危险等级可配置

## Skill 系统

`bot/extension/skill/`：
- YAML 格式技能定义
- 目录发现 + 加载
- 内置技能通过 `bundled/` go:embed
- `skill` 工具动态注册到 toolRegistry
- 插件可提供额外 Skill 目录

## 子 Agent 系统

### 内置类型（3 种）

| Agent | 用途 | 工具 | 特殊配置 |
|-------|------|------|---------|
| executor | 执行代码修改 | read/write/edit/bash/grep/glob/list | — |
| verify | 验证修改 | read/grep/glob/list/bash | — |
| researcher | 代码探索/调研 | read/grep/glob/list/web_search/web_fetch | OmitProjectContext: true |

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
- 子 Agent 并发通过 `subslot.Manager` 管理（最大 8 并发 + 颜色分配）

### AgentMD 解析

`bot/agent/subagent/agent_md.go`：解析 Claude Code 格式的 `agents/*.md`（YAML frontmatter）。

## 治理系统

### Ledger（工具执行账本）

`bot/agent/ledger/`：追踪所有工具执行事件，记录：
- `readFiles`：已读取文件集合
- `modifiedFiles`：已修改文件集合
- `blockedTools`：被阻止的工具调用
- `toolErrors`：工具执行错误
- `verifications`：验证记录

### ResponseGate（响应门控）

`bot/agent/gate/`：防止治理内部信号泄漏到模型可见输出。默认最多 2 次重试。

### 工具语义分类

`bot/governance/semantics.go`：定义工具语义标签：
- `SourceProducing`：产生源码信息（read/grep/glob/list）
- `Mutating`：修改文件（write/edit/bash）
- `Verifying`：验证操作

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
| Bot 应用层 | `bot/app/` | 依赖注入 + 生命周期编排 + BotInterface 实现 |
| Agent 循环 | `bot/agent/runtime/` | Reason→Execute→Feedback，中断，重试 |
| 治理系统 | `bot/agent/governance/` | GovManager：HookReg + Ledger + Exploration + Gate |
| 工具账本 | `bot/agent/ledger/` | 工具执行追踪（读/写/阻止/错误/验证） |
| 响应门控 | `bot/agent/gate/` | 治理重试限制 |
| 推理格式 | `bot/agent/reasoning/` | GarbledToolCall 检测 |
| 子 Agent | `bot/agent/subagent/` | 独立循环，3 种内置类型 + 插件扩展 |
| 子槽位 | `bot/agent/subslot/` | 并发控制（8 槽位 + 颜色） |
| 预算配额 | `bot/agent/budget/` | 探索检测 + 工具配额 |
| LLM 网关 | `llm/` | OpenAI/Anthropic 双协议，统一接口 |
| 工具系统 | `bot/tools/` | Tool 接口 + Executor + Registry + FileCache |
| 工具注册 | `bot/tools/catalog/` | RegisterAll() 内置工具注册清单 |
| 工具执行 | `bot/tools/runner/` | 执行引擎（单工具/批量/预览） |
| 编辑 DSL | `bot/tools/editdsl/` | 编辑锚点 · 哈希计算 · Patch DSL · recovery |
| 文件系统工具 | `bot/tools/filesystem/` | read/write/edit/list/tree/glob/grep |
| Shell 工具 | `bot/tools/shell/` | bash 执行与风险分级 |
| Web 工具 | `bot/tools/web/` | web_search/web_fetch/html2md |
| 媒体工具 | `bot/tools/media/` | image_gen（即梦文生图） |
| 任务工具 | `bot/tools/tasktool/`, `bot/tools/todo/` | sub-agent task 与 todo_write |
| SDK | `bot/sdk/` | 外部服务 SDK（火山引擎 SigV4 签名） |
| 上下文管理 | `bot/contextmgr/` | Build 管线 + 五级压缩 + token 估算 |
| Session Memory | `bot/contextmgr/memory/` | Memory 文件持久化 |
| Plugin 系统 | `bot/extension/plugin/` | 安装/卸载/生命周期 |
| MCP 客户端 | `bot/extension/mcp/` | JSON-RPC 2.0 |
| Skill 系统 | `bot/extension/skill/` | YAML 技能加载 + 工具注册 |
| Hook 系统 | `bot/hooks/` | 事件驱动（7 种触发点）+ 声明式（plugin/） |
| 内置 Hook | `bot/hooks/builtin/` | 8 个内置 Hook 实现 |
| 声明式 Hook | `bot/hooks/plugin/` | JSON 配置驱动 Hook |
| Tree-sitter | `bot/treesitter/` | 多语言解析器注册 + AST 查询 |
| 代码索引 | `bot/index/` | SQLite + FTS5 + Tree-sitter 代码索引 |
| 命令系统 | `bot/command/` | 斜杠命令解析 |
| 调试日志 | `bot/debug/` | 全局 debug.Log（时间戳 + subagent 标签） |
| 工具语义 | `bot/governance/` | Semantics 分类（SourceProducing/Mutating/Verifying） |
| Plugin CLI | `bot/plugincli/` | 插件 CLI 辅助（源解析/格式化/远程获取） |
| Session 视图 | `bot/sessionview/` | DisplayMessages 转换 |
| TUI | `tui/` | Bubble Tea v2 组件化 |
