# NekoCode 开发路线

> **本文档职责**: 追踪已完成和待办的功能项。记录开发里程碑、实施状态（✅/🟡）。每项简要描述功能目标，不展开设计或架构细节（细节属于 DESIGN.md / ARCHITECTURE.md）。更新时请保持此边界。

按优先级排列，每项可独立实施验证。✅ = 已完成，🟡 = 部分完成。

---

## P0 — 核心功能

### 1. 精确编辑工具 (EditTool) ✅
### 2. 内容搜索工具 (GrepTool) ✅
### 3. Diff 展示 ✅
### 4. 结构化内容块 (ContentBlock) ✅
### 5. TUI-Bot 解耦 ✅
- `bot.UI` 契约，TUI 不直接依赖 bot 内部实现

### 6. 项目感知上下文 ✅
- NEKOCODE.md 自动发现 + @include 递归加载

### 7. Web Search / Fetch ✅

---

## P1 — 架构增强

### 8. Provider 合并 ✅
### 9. 上下文窗口优化 ✅
### 10. 微压缩 ✅
### 11. Session Memory ✅
### 12. Snip 工具 ✅ → 已移除
### 13. `/new` 命令 ✅
### 14. 共享 HTTP 客户端 ✅
### 15. 确认框 ✅ — 含插件安装确认
### 16. ANSI 清理 ✅
### 17. 并行工具执行 ✅
### 18. 处理阶段 ✅
### 19. Scrollbar 独立组件 ✅
### 20. BTW 中断机制 ✅
### 21. 指数退避重试 ✅
### 22. 模块重组 ✅
### 23. 子 Agent 系统 ✅ — 含 AgentMD 解析 + Handoff + Compactor 接入 + 文件缓存预热
### 24. 任务列表 (Todo tracking) ✅
### 25. 代码质量 ✅
### 26. 输出噪声过滤 ✅
### 27. 文档更新 ✅
### 28. 幻觉防治体系 ✅
### 29. Skill 系统 ✅
### 30. 上下文锚点 ✅
### 31. 摘要验证 ✅
### 32. 文件缓存 ✅
### 33. 五级预警自动压缩 ✅
### 34. 对话历史存取 ✅
### 35. NEKOCODE.md 项目上下文 ✅
### 36. 子包拆分：config + command ✅
### 37. 工具实现重组 ✅
### 38. TUI handlers 合并 ✅
### 39. Markdown 渲染移动 ✅
### 40. Bundled Skills ✅
### 41. 分层 System Prompt ✅
### 42. 工具描述精准化 ✅
### 43. TodoWrite 一等公民 ✅
### 44. Prompt 前缀缓存优化 ✅ — DeepSeek 自动 KV 缓存 + 上下文窗口拉满
### 45. LLM 协议解耦 ✅ — OpenAI/Anthropic 双协议兼容，删除 Anthropic API 依赖
### 46. Plan Mode ✅
### 47. Auto-Review 自动验证 ✅
### 48. Bug 分析反幻觉 ✅
### 49. 智能截断 ✅
### 50. 任务列表 UI 主题化 ✅
### 51. 长任务友好 ✅
### 52. Edit 模糊匹配修复 ✅
### 53. 子 Agent 安全分类器 ✅
### 54. LLM Factory + Clone 一致性 ✅
### 55. 流式 HTTP 客户端超时 ✅
### 56. 智能重试判定 ✅
### 57. Thinking 控制统一 ✅ — SetDisableThinking 跨协议一致
### 58. Bot 生命周期管理 ✅

### 59. UI 契约精简 ✅
- `RunAgent + SetCallbacks` 合并为 `Run(input, callbacks)`
- `bot.UI` / `bot.GUI` 由 bot 层统一暴露，TUI/GUI 共享契约

### 60. 声明式 Hook 系统 ✅
- 6 种事件类型，JSON 配置驱动（hooks.json），支持 tool name matcher + 变量展开

### 61. Plugin 系统 ✅
- GitHub URL / user:repo / 本地路径三种安装源，自动发现扩展点，异步安装 + 通知

### 62. MCP 客户端 ✅
- JSON-RPC 2.0 协议，Server 生命周期管理，工具暴露为 `tools.Tool` 接口

### 63. AgentMD 解析 ✅
- 解析 Claude Code 格式的 agents/*.md（YAML frontmatter），插件通过注册表提供 agent

### 64. 全局调试日志 ✅
- 统一日志输出到 `~/.nekocode/logs/nekocode-debug.log`，替代原 compact/log.go

### 65. Context 统计优化 ✅
- `/context` 命令彩色 bar + 精简统计摘要

### 66. 输入框修复 ✅
- 自动换行、视觉行导航、viewport 稳定性修复

### 67. 子 Agent 结果简化 ✅
- 删除结构化输出解析，Result 结构体精简，FreeOutput 改为默认行为

### 68. ctxmgr 模块精简 ✅
- 删除死方法和字段，提取 build.go，全局日志独立为 debug 包

### 69. interface{} → any ✅
- 全项目替换，零残留

---

### 70. Session 持久化 ✅
- Snapshot/Restore 机制
- 对话存档恢复 + 分支对话

### 71. View 视图拆分 ✅
- 从 model.go 拆分出 view.go（`tea.View` 布局组装）

### 72. SubAgent 代码清理 ✅
- 删除 classify.go（安全审核内联到 result.go）
- 删除废弃 prompt 文件（decompose/explore/output_format/plan/shared_rules）
- Result 结构体精简

### 73. Agent 模块清理 ✅
- 删除 agent/log.go（日志统一到 debug 包）
- 删除 agent/retry.go（重试逻辑内联到 reason.go）

### 74. ctxmgr 模块清理 ✅
- 删除 context/meta.go（元数据格式化内联到 content.go）
- 删除 compact/log.go（日志统一到 debug 包）

### 75. Prompt 模块清理 ✅
- 删除 prompt/plan.go（Plan mode prompt 内联到 lifecycle.go）

### 76. 文生图工具 (ImageGen) ✅
- 即梦 Jimeng 文生图 3.1 API 接入
- 火山引擎 SigV4 签名 SDK 封装（`bot/sdk/`）
- `image_gen` 工具：提交任务 → 轮询 → 下载保存本地
- 配置：`image_gen_models` 数组，支持多文生图模型扩展

### 77. 代码索引 (Index) ✅
- Tree-sitter 多语言解析（Go/JS/TS/Python/Rust），提取函数/方法/类/结构体/接口/变量/常量
- 代码知识图谱：符号关系图（calls/contains/imports），跨文件引用解析
- 5 种查询模式：skeleton（项目概览）、symbol（符号查找）、deps（包依赖）、file（文件搜索）、search（FTS5 全文搜索）
- 增量同步：fsnotify 文件监听 + 内容哈希 + 500ms 防抖
- SQLite 持久化 + FTS5 全文索引，无 FTS5 时自动降级为内存模式
- 替代原 `projctx` 模块，集成 `project_info` tool

### 78. SQLite 纯 Go 化 ✅
- 替换 go-sqlite3（依赖 CGO）为 zombiezen.com/go/sqlite（纯 Go 实现）
- 消除 C 编译器依赖，简化交叉编译，提升跨平台兼容性

### 79. 内容锚定编辑升级 ✅
- edit 使用 oldString/newString 直接锚定当前文件内容
- 移除 VIEW/windowId/baseRevision 协议和 ViewStore 链路
- 保留结构化 diff preview、自动快照、撤销和 gofmt lint

### 80. TUI 升级 ✅
- Bubble Tea v2 生态全面升级
- 组件渲染优化，消息列表性能改进

### 81. 多项 Bug 修复 ✅
- 编辑工具模糊匹配修复
- 子 Agent 结果处理修复
- Agent 循环稳定性改进

### 82. 结构化 Diff 模型 ✅
- `diff_model.go`：`EDIT_PREVIEW_JSON_B64` base64 编码结构化 diff
- TUI 可直接解析渲染，替代纯文本 diff 预览
- 支持 kind/line_no/text 三字段精确描述每行变更

### 83. Edit Lint 集成 ✅
- `edit_lint.go`：编辑 `.go` 文件后自动执行 gofmt 语法检查
- 发现语法错误时注入 `[System]` 提示，防止错误积累
- 框架可扩展支持其他语言的 linter

## P1.5 — Agent 治理层

### Agent Governance Layer 🟡
- 已完成：ToolSemanticClassifier、AgentLedger、FinalCheck 基础规则、Hook Policy Action 骨架、PreToolUse BlockTool、PostTurn BlockFinal/RequireTool、verificationHook 硬化
- 已完成：ctxmgr 自动压缩锁修复、Index stale/race 修复、ImageGen base64 保存、探索配额统一语义分类
- 已完成：8 内置 Hook（quota / verification / exploration_exhausted / exploration_guard / explore_cascade / progress_stall / completion_quality / garbled_circuit_breaker）
- 待推进：Plugin Hook 输出治理；System Prompt 分层瘦身

## P2 — 生态与体验

### 84. 后台任务 + 进度
### 85. Checkpoint / Undo
### 86. 凭证管理
### 87. 自动化测试
- Agent 行为回归测试（mock LLM 响应）
- 工具执行单元测试（mock 文件系统/shell）
