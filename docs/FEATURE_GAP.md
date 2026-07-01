# NekoCode 功能对比报告

> 基于对 `/home/lznauy/precode/NekoCode` 源码的逐模块审查，与 Claude Code 成熟能力进行对比，分析 NekoCode 当前已实现与缺失的功能。

---

## 一、已实现功能总览

### 工具系统（13 个内置工具 + 3 个条件/动态工具）

| 工具 | 文件 | 说明 |
|------|------|------|
| `bash` | shell/tool_bash.go | Shell 执行，四级危险分类（LevelSafe/Write/Destructive/Forbidden），heredoc 剥离 |
| `read` | filesystem/read/tool_read.go | 文件读取（文本/图片/PDF），支持行范围，输出 `[path#TAG]` 和行号内容 |
| `write` | filesystem/write/tool_write.go | 文件创建/覆写，自动创建父目录 |
| `edit` | filesystem/edit/tool_edit.go | oldString/newString 内容锚定编辑，唯一匹配校验，自动快照 + 撤销 + gofmt lint |
| `list` | filesystem/list/tool_list.go | 目录列表 |
| `tree` | filesystem/tree/tool_tree.go | 目录树，支持深度/条目限制 |
| `glob` | filesystem/search/tool_glob.go | 文件 glob 匹配，支持 `**` 递归 |
| `grep` | filesystem/search/tool_grep.go | 内容搜索（优先 rg，fallback grep），支持正则 + glob 过滤 + 上下文行 |
| `web_search` | web/tool_websearch.go | 网页搜索 |
| `web_fetch` | web/tool_webfetch.go | 网页抓取，URL 验证 + 重定向限制 |
| `question` | question/tool_question.go | 向用户提问（结构化问题，支持单选/多选/自定义输入，等待回答） |
| `todo_write` | todo/tool_todo.go | 任务列表管理，支持 GUI/TUI 回调 |
| `task` | tasktool/tool_task.go | 子 Agent 调度（researcher/executor/verify） |
| `project_info` | index/projecttool/tool.go | 代码索引查询（符号/文件/依赖/全文搜索）— 条件注册 |
| `image_gen` | media/tool_image_gen.go | 图片生成（多模型配置）— 条件注册 |
| `skill` | extension/skill/tool_skill.go | 技能加载工具 — 动态注册 |
| MCP 工具 | extension/mcp/tool.go | 动态注册的 MCP 服务器工具 |

### Agent 系统

- **主循环**：消息驱动，PreTurn → Reason → Execute → PostTurn → Stop 完整生命周期
- **推理器**：LLM 调用 + 响应解析，支持 tool calls 和纯文本两种模式
- **工具执行**：quota 过滤 → PreToolUse hook → 执行 → PostToolUse hook → 结果反馈
- **子 Agent**：独立 Engine，支持 researcher/executor/verify 三种类型，文件缓存隔离
- **预算管理**：ExplorationTracker 衰减分数机制（200 分起，工具扣分，edit 恢复）
- **账本记录**：Ledger 追踪读取/修改文件、被阻止工具、验证结果
- **策略分类**：Semantics 分类（Exploratory/Mutating/Verifying/SourceProducing 等）
- **安全防护**：maxAgentSteps=150、maxConsecutiveHints=3、maxConsecutiveFailures=5、maxFinalCheckHints=2
- **Edit 后 Lint**：`.go` 文件编辑后自动 gofmt 检查，发现语法错误及时注入提示

### 上下文管理

- **分层架构**：Layer 0（系统提示词 + 记忆）→ Layer 0.5（Archive 摘要）→ 消息层
- **自动压缩**：Head-Tail-Summary 重建，保留最近 3 轮，旧消息 LLM 摘要
- **Token 追踪**：估算 token 用量，触发自动压缩
- **持久化记忆**：结构化 markdown 文件（Tech Stack / Active Goals / Completed Tasks / Architecture Map / Preferences）
- **子 Agent 上下文**：独立 Manager，可注入项目上下文 + 工作目录

### Hook 系统

- **7 个 Hook 点**：PreTurn、PreToolUse、PostToolUse、PostTool、PostTurn、UserSubmit、Stop
- **Hook 能力**：注入 Hint、阻止工具、要求工具、阻止最终输出、状态补丁
- **内置 Hook**：8 个（quota / verification / exploration_exhausted / exploration_guard / explore_cascade / progress_stall / completion_quality / garbled_circuit_breaker）
- **插件 Hook**：支持从外部插件加载声明式 Hook

### TUI 界面

- **框架**：Bubble Tea + Lipgloss
- **Markdown 渲染**：chroma/glamour 库，tokyo-night 主题
- **Diff 预览**：edit 工具结果中展示增删行（绿色/红色背景）+ 结构化 diff 模型
- **鼠标支持**：滚轮滚动消息列表
- **命令补全**：Tab/Shift+Tab 选择，`/` 弹出补全菜单
- **组件**：消息列表、输入框、确认栏、滚动条、块渲染、处理状态、splash 屏
- **子 Agent 显示**：颜色编码的子 Agent 状态

### 其他

- **代码索引**：tree-sitter 多语言解析 + 图数据库 + 符号/依赖/全文搜索
- **插件系统**：manifest 解析 + 命令注册 + Hook 注册 + 子 Agent 注册
- **技能系统**：bundled 技能 + 文件加载 + 工具化 + 上下文注入
- **命令系统**：`/help`、`/clear`、`/summarize`、`/new`、`/context`（含原 /stats 功能）、`/config`、`/model`、`/plan`、`/plugin`、`/sessions`、`/export` 共 11 个命令（含动态 skill 命令）
- **MCP 客户端**：stdio 子进程模式，JSON-RPC 通信，工具发现
- **LLM 层**：Anthropic + OpenAI 双协议，流式 API，重试机制
- **配置**：provider/model/apiKey/baseURL + 图片生成模型配置
- **结构化 Diff 模型**：`EDIT_PREVIEW_JSON_B64` base64 编码 diff，TUI 直接解析渲染

---

## 二、功能缺失清单

### 🔴 P0 — 阻碍基本可用性

#### 1. Bash 安全机制薄弱

```
当前：关键词模式匹配四级分类（LevelSafe / LevelWrite / LevelDestructive / LevelForbidden）
缺失：
  ❌ Bash AST 解析器 — 无法理解命令语法树，只能做字符串/前缀匹配
  ❌ 路径约束检查 — 无法限制文件访问范围（如只允许项目目录内操作）
  ❌ 沙箱执行 — 无容器/隔离环境执行
  ❌ 权限规则持久化 — 无法记住用户的 allow/deny 决定
  ❌ 权限分类器 UI — 无交互式权限确认界面
```

#### 2. 权限系统缺失

```
Claude Code 权限系统庞大，NekoCode 目前仅有基础确认：
  ✅ 工具级确认弹框（safe/modify/danger/blocked 四级）
  ❌ allow/deny 规则持久化
  ❌ 权限规则匹配引擎
  ❌ 自动模式（auto-approve）
  ❌ 权限 UI 交互（一次性记住选择）
```

#### 3. CLI 主入口仍需完善

```
当前：TUI (cmd/nekocode-tui) + GUI (main.go) 两个入口
已实现：
  ✅ 双前端入口分离
  ✅ 配置文件读取
缺失：
  ❌ 子命令系统（init/config/run/doctor/update...）
  ❌ 命令行参数解析（--model, --config, --debug...）
  ❌ 版本信息（-v/--version）
  ❌ 帮助系统（-h/--help）
  ❌ 信号处理（优雅关闭 SIGINT/SIGTERM）
```

#### 4. 工具种类不足

```
已有 13 内置 + 3 条件/动态工具，缺失的关键工具：
  ❌ LSP 工具 — 跳转定义、查找引用、诊断
  ❌ notebook 编辑 — Jupyter notebook 支持
  ❌ task 子工具 — task_list/get/update/stop/output（当前只有 task 创建）
  ❌ MCP 资源工具 — list_mcp_resources / read_mcp_resource
  ❌ 定时任务 — schedule_cron
  ❌ config 工具 — 读写配置
  ❌ 独立 diff 工具 — 代码变更对比（当前 diff 是 edit 内部辅助）
```

---

### 🟡 P1 — 影响核心体验

#### 5. TUI 功能不完整

```
已有：Markdown 渲染（glamour + chroma 语法高亮）、diff 预览、鼠标滚轮、命令补全、基础组件、
      token 用量仪表盘（/context 显示 bar + used/total + sys/tools/todo/skills/msgs 分解）
缺失：
  ❌ 文件树浏览器 — 无侧边栏文件浏览
  ❌ 多面板布局 — 无分屏（代码+对话+终端）
  ❌ 进度指示器 — 长时间操作无进度条
  ❌ 主题切换 — 仅 tokyo-night 硬编码
  ❌ 快捷键提示栏 — 无底部状态栏
  ❌ 搜索界面 — 无交互式搜索结果浏览
```

#### 6. LLM 层功能不足

```
已有：Anthropic + OpenAI 双协议、流式 API、重试机制、Thinking 跨协议控制
缺失：
  ❌ 模型路由 — 无法按任务类型自动选择模型
  ❌ Fallback 机制 — API 失败时无法自动切换备用模型
  ❌ 并发控制/限流 — 无 API 调用速率限制
  ❌ 精确 token 计数 — 使用估算而非各模型专用 tokenizer
  ❌ Google Gemini 支持 — 目前仅支持 OpenAI/Anthropic 兼容协议
  ❌ 请求队列 — 无请求排队和优先级
```

#### 7. 上下文管理不完整

```
已有：Head-Tail-Summary 压缩、持久化记忆（手动 Build/Save）、token 追踪、五级预警、micro-compact 优先级裁剪（priorityLow/Medium/High）
部分实现：
  ⚠️ 摘要验证 — DESIGN.md 声称"二次校验"，但代码仅实现 XML block 提取（FormatCompactSummary），无实际验证逻辑
缺失：
  ❌ 记忆自动更新 — DESIGN.md 描述"10k token 后异步提取"，但代码仅实现手动 Build/Save，无自动触发机制
  ❌ 智能上下文裁剪 — 基于内容相关性而非简单按时间
  ❌ 分层上下文 — 项目级/文件级/代码块级结构
  ❌ RAG 集成 — 无向量数据库检索增强
```

#### 8. 命令系统可扩展

```
已有（11 个命令，含动态 skill）：
  /help、/clear、/summarize、/new、/context（含原 /stats 功能）、/config、/model、/plan、/plugin、/sessions、/export
缺失：
  ❌ 命令别名
  ❌ 命令历史搜索
  ❌ /review、/commit、/diff、/doctor、/cost、/status、/resume、/init 等
  ❌ 命令权限分级
```

---

### 🟢 P2 — 产品化完善

#### 9. MCP 客户端

```
已有：stdio 子进程模式
缺失：
  ❌ SSE 传输 — 无法连接远程 SSE MCP 服务器
  ❌ StreamableHTTP 传输
  ❌ OAuth 认证
  ❌ 服务发现
  ❌ 协议版本协商
  ❌ 多服务连接池
  ❌ 健康检查 + 自动重连
  ❌ MCP Resources/Prompts 支持（仅 Tools）
```

#### 10. 插件系统

```
已有：manifest 解析 + 命令/Hook/Agent 注册 + install/uninstall/enable/disable
缺失：
  ❌ 插件市场/包管理
  ❌ 插件依赖管理
  ❌ 沙箱隔离
  ❌ 配置界面
  ❌ 自动更新
  ❌ 插件权限声明
```

#### 11. 技能系统

```
已有：bundled 技能 + 文件加载 + 工具化
缺失：
  ❌ 技能市场
  ❌ 链式组合/编排
  ❌ 参数 Schema 验证
  ❌ 热加载/卸载
  ❌ 自动化测试框架
```

#### 12. 会话管理

```
已有：创建 + 存储 + 列出 + 恢复 + 导出
缺失：
  ❌ 会话历史浏览
  ❌ 会话分支/合并
  ❌ 会话自动过期清理
  ❌ 会话搜索
```

#### 13. 配置系统

```
已有：基础 provider/model/apiKey/baseURL 配置 + image_gen_models
缺失：
  ❌ 热重载
  ❌ 分层覆盖（默认/用户/项目级）
  ❌ 敏感配置加密存储
  ❌ 配置 Schema 验证
  ❌ 多环境支持
  ❌ 配置导出/导入
```

#### 14. 基础设施

```
缺失：
  ❌ 统一日志框架（仅有 debug.Log + panic 恢复）
  ❌ 错误码体系
  ❌ 全局事件总线
  ❌ goroutine 工作池
  ❌ 请求限流器
  ❌ 通用重试机制（仅 LLM 层有）
  ❌ 健康检查端点
  ❌ 指标/监控
```

---

## 三、NekoCode 的独特优势

相比 Claude Code，NekoCode 有以下亮点：

1. **代码索引系统（index）** — 自研 tree-sitter 多语言解析 + 图数据库，支持符号搜索、依赖分析和全文搜索，Claude Code 依赖 LSP 无此独立能力
2. **完善的测试覆盖** — 各模块均有测试代码，Claude Code 几乎无测试
3. **Go 语言实现** — 编译为单一二进制，部署简单，性能优异，内存安全
4. **架构清晰** — 模块边界明确，依赖关系简洁，易于理解和贡献
5. **双前端架构** — TUI + GUI 共享 Bot 核心，`bot.UI` / `bot.GUI` 契约解耦
6. **Hook 系统成熟** — 7 事件点 + 8 内置 Hook + 声明式插件 Hook
7. **纯 Go SQLite** — 零 CGO 依赖，简化交叉编译
8. **Edit Lint 集成** — Go 文件编辑后自动 gofmt 检查，防止语法错误积累
9. **品牌一致性** — 独立设计的猫娘主题 logo + 全套平台图标资源（icns/ico/png），GUI/TUI 视觉统一
10. **Prompt 编码纪律** — 针对 LLM 编码四大毛病（Karpathy 观察）的系统性 prompt 约束，覆盖主 agent + 子 agent + plan mode

---

## 四、优先级建议

```
P0（必须立即补齐，否则无法作为 AI 编程助手使用）：
  1. Bash 安全增强（AST 解析 + 路径约束 + 沙箱）
  2. 权限系统（allow/deny 持久化规则）
  3. 补充核心工具（LSP、task 子工具）
  4. 完善 CLI 入口（子命令 + 参数解析 + 帮助系统）

P1（影响核心体验，应尽快实现）：
  5. 文件树 + 多面板
  6. LLM 模型路由 + Fallback + 更多模型支持
  7. 智能上下文裁剪 + RAG 集成
  8. 更多命令（/review、/commit、/diff 等）

P2（产品化完善，可逐步迭代）：
  9. MCP SSE/HTTP 传输 + OAuth
  10. 插件市场 + 沙箱隔离
  11. 会话分支 + 自动清理
  12. 主题系统 + 配置热重载
  13. 日志/监控/健康检查基础设施
```
