# NekoCode 🐱

<p align="center">
  <b>开源 · 多模型自由 · 猫娘角色 · Go 单二进制 · 可嵌入的 Agent 核心</b>
</p>

<p align="center">
  <sub>多模型自由 · Agent 循环 · 子 Agent 委派 · 上下文管理 · 会话记忆 · MCP + Plugin + Skill 生态</sub>
</p>

<br>

<table>
<tr>
<td width="50%"><img src="docs/images/splash.png" width="100%" alt="启动页"></td>
<td width="50%"><img src="docs/images/chat.png" width="100%" alt="聊天界面"></td>
</tr>
</table>

---

## 设计理念

**模型自由，不站队**

MIT 开源，代码公开可审计。原生 Anthropic 协议 + OpenAI 兼容协议统一网关接入，兼容 DeepSeek、MiniMax 等 OpenAI 协议模型。

**终端也可以好看**

厚左色条角色配色、工具卡片折叠展开、diff 高亮内联、思考过程实时分区展示——每个交互细节都经过打磨。终端不是妥协，是选择。

**纵深防御幻觉**

从 System Prompt 约束、运行时强制校验（先读后改、二进制检测）、Hook 实时 hint 注入（重复调用检测、探索枯竭预警、乱码检测、未完成工作提醒）、断路器（连续乱码终止）、独立验证 agent、记忆漂移防护、来源引用强制，到上下文保真压缩——每一层独立生效，层层兜底。

**越聊越懂你**

长对话自动提取结构化笔记——目标、进度、关键决策、下一步行动——写入本地。开新对话时自动注入，零额外 API 消耗，助理永远记得上次聊到哪了。

**一处编写，处处接入**

Bot 核心通过 `BotInterface` 接口与 UI 完全解耦。同样的 Agent，今天跑在终端 TUI 里，明天可以接入 Web GUI、桌面应用、甚至 IM 消息平台——逻辑不改，只换壳。


## 功能

| | | | |
|:--|:--|:--|:--|
| **对话** | 自然语言交互 · 猫娘角色 | **Shell** | 命令执行 · 4 级安全分级 · 智能降级 |
| **文件** | 读取 · 写入 · 精确编辑 + diff | **搜索** | glob 模式 · ripgrep 内容搜索 · 网页搜索/抓取 |
| **子 Agent** | 3 种类型独立委派（executor/researcher/verify） | **记忆** | 结构化笔记提取 · 跨对话复用 |
| **确认** | 写入/危险操作弹框确认 · 高危命令直接拒绝 | **命令** | `/` 斜杠命令 · 实时补全 |
| **折叠** | 工具组折叠 · diff 展开 | **多模型** | Anthropic / OpenAI 协议 · 兼容 DeepSeek / MiniMax · 运行时切换 |
| **工具** | 14 个内置工具（bash/read/write/edit/glob/grep/list/tree/task/todo_write/project_info/web_search/web_fetch/image_gen） | **上下文** | 五级预警水位 · 分层压缩 · 锚点保留 |
| **Hook** | 8 个内置 Hook + 声明式 · 7 种事件点 | **Governance** | 语义分类 · Agent 账本 · 最终校验 · 策略门禁 |
| **Plugin** | 安装/卸载/启用 · GitHub/本地 · Claude Code 兼容 | **MCP** | 外部 MCP server 工具扩展 · JSON-RPC 2.0 |
| **Plan** | `/plan` 只读探索 · 审批执行 | **Debug** | 全局调试日志 · 上下文统计 |
| **Session** | 对话存档恢复 | **Project** | NEKOCODE.md 项目约定感知 · @include 递归加载 |
| **Todo** | 任务跟踪 · 自动更新状态 | **目录** | 目录树 + 列表浏览 |
| **Project Info** | 代码索引（symbol/deps/file/search/skeleton）· Tree-sitter 多语言 · SQLite 持久化 · 增量同步 | **文件缓存** | LRU + mtime 去重 · 跨子 Agent 共享 |
| **Image Gen** | 文生图（即梦/jimeng_t2i_v31）· 自动下载保存本地 · 支持多模型配置 | **Skill** | 内置 bundled skill · YAML 定义 · 社区技能包 |


---

## 命令

| 命令 | 说明 |
|------|------|
| `/help` | 显示命令列表 |
| `/new` | 新对话（保留会话记忆） |
| `/clear` | 清空所有历史 |
| `/stats` | 上下文用量统计 |
| `/summarize` | 手动压缩记忆 |
| `/config` | 当前 provider / model |
| `/context` | 上下文彩色 bar + 统计摘要 |
| `/plugin` | 插件安装/卸载/列表/详情 |
| `/plan <任务>` | 只读探索模式，设计方案后审批执行 |
| `/sessions` | 会话管理：列表、恢复存档 |
| `/export` | 导出对话上下文到 JSON 文件 |
| `/model` | 列出或切换模型（`/model <name>`） |
| `/<skill>` | 加载技能工作流（每个已加载 skill 自动注册） |


输入 `/` 自动弹出补全，Tab 选择，Enter 填入。

---

## 安全分级

| 等级 | 行为 | 示例 |
|:--|:--|:--|
| `safe` | 自动放行，无需确认 | `read` `glob` `grep` `list` `tree` `git log` |
| `modify` | 弹框确认 | `write` `edit` `bash` `mkdir` |
| `danger` | 红色警告确认 | `rm` `kill` `git push -f` |
| `blocked` | 直接拒绝 | `sudo` `curl\|bash` `ssh` `dd` |

bash 命令智能识别——`go vet`、`git diff` 等纯输出命令自动降级为 safe，不用每次确认。

---

## 架构

```
┌──────────────────────────────────────────────────────┐
│              TUI / GUI / IM                          │  ← 任意前端，通过接口对接
│        BotInterface (12 方法)                        │  ← 接口契约
├──────────────────────────────────────────────────────┤
│           Bot Core (独立模块)                        │
│  ┌──────────┐  ┌──────────────────┐                  │
│  │ Agent循环│  │  上下文管理器    │                  │
│  │ Reason→  │  │  五层压缩+锚点   │                  │
│  │ Execute→ │  │  五级预警水位    │                  │
│  │ Feedback │  │  会话记忆        │                  │
│  └──────────┘  └──────────────────┘                  │
│  ┌──────────┐  ┌──────────────────┐                  │
│  │ 子 Agent │  │  工具系统 (14)   │
│  │ 3 种类型 │  │  bash/read/write │                  │
│  │ AgentMD  │  │  edit/glob/grep  │                  │
│  │ 独立引擎 │  │  list/tree/task  │                  │
│  └──────────┘  │  todo/proj_info  │                  │
│  ┌──────────┐  │  web_search/fetch│                  │
│  │Image Gen │  │  skill/image_gen │                  │
│  │ 文生图   │  └──────────────────┘                  │
│  │ 即梦接入 │  ┌──────────┐  ┌──────────┐            │
│  └──────────┘  │ Hook引擎 │  │Governance│            │
│  ┌──────────┐  │ 8 内置   │  │ 语义分类 │
│  │ Skill引擎│  │ 声明式   │  │ Agent账本│
│  │ 技能包   │  │ 事件驱动 │  │ 最终校验 │            │
│  │ 内置+社区│  │ 7 事件点 │  │ 策略门禁 │            │
│  │ YAML定义 │  └──────────┘  └──────────┘            │
│  └──────────┘  ┌──────────┐  ┌──────────┐            │
│  ┌──────────┐  │  Token   │  │  Plugin  │            │
│  │  CIndex  │  │  预算管理 │  │  安装/卸载│           │
│  │ 代码索引 │  │  探索分数 │  │  Claude   │           │
│  │ NEKOCODE │  │  回环检测 │  │  Code兼容 │           │
│  │ .md      │  └──────────┘  └──────────┘            │
│  │ Tree-    │  ┌──────────┐  ┌──────────┐            │
│  │ sitter   │  │  MCP     │  │  Debug   │            │
│  │ SQLite   │  │  JSON-RPC│  │  全局日志 │           │
│  └──────────┘  │  外部工具 │  │  自动轮转 │           │
│  ┌──────────┐  └──────────┘  └──────────┘            │
│  │ 命令解析 │                                        │
│  │ 斜杠命令 │                                        │
│  │ 动态注册 │                                        │
│  └──────────┘                                        │
├──────────────────────────────────────────────────────┤
│          LLM 统一网关                                │
│  Anthropic / OpenAI 协议 · 兼容 DeepSeek / MiniMax   │
│  Prompt Caching / Prefix Cache · Thinking 互译       │
└──────────────────────────────────────────────────────┘
```
Bot 核心不依赖任何特定 UI 框架。`BotInterface` 定义了完整的 Agent 交互契约——发送消息、流式回调、工具确认、中止控制、模型切换、会话恢复。换个前端只需实现这 12 个方法。

> **五级预警水位**：Normal → Warning → MicroCompact → Compact → Blocking，逐级触发更激进的上下文压缩策略。

## 快速开始

> 需要 Go 1.25+
```bash
# 1. 创建配置
mkdir -p ~/.nekocode
cat > ~/.nekocode/config.json << 'EOF'
{
  "active": "deepseek",
  "context_window": 128000,
  "flash_model": "deepseek",
  "models": [
    {
      "name": "deepseek",
      "provider": "deepseek",
      "api_key": "sk-your-key-here",
      "model": "deepseek-chat",
      "base_url": "https://api.deepseek.com/v1",
      "protocol": "openai"
    },
    {
      "name": "claude",
      "provider": "anthropic",
      "api_key": "sk-your-key-here",
      "model": "claude-sonnet-4-20250514",
      "base_url": "https://api.anthropic.com/v1",
      "protocol": "anthropic"
    }
  ]
}

EOF
# 2. 构建
go build -o nekocode ./cmd/

# 3. 运行
./nekocode
```

### 配置说明

| 字段 | 说明 | 必填 |
|------|------|------|
| `active` | 当前激活的模型名称（对应 models 中的 name） | 是 |
| `context_window` | 上下文窗口大小（token） | 是 |
| `flash_model` | 轻量模型名称，用于子 Agent 和摘要 | 否 |
| `models` | 模型配置数组，支持多模型切换 | 是 |
| **models[].name** | 模型标识名（用于 /model 切换） | 是 |
| **models[].provider** | LLM 提供商：`deepseek` / `anthropic` / `minimax` 等 | 是 |
| **models[].api_key** | API 密钥 | 是 |
| **models[].model** | 模型名称 | 是 |
| **models[].base_url** | API 端点地址 | 是 |
| **models[].protocol** | 协议类型：`openai`（默认）或 `anthropic` | 否 |
| `image_gen_models` | 文生图模型配置数组 | 否 |
| **image_gen_models[].name** | 模型标识名 | 是 |
| **image_gen_models[].provider** | 文生图提供商：`jimeng`（即梦/火山引擎） | 是 |
| **image_gen_models[].api_key** | 火山引擎 Access Key ID | 是 |
| **image_gen_models[].secret_key** | 火山引擎 Secret Access Key | 是 |
| **image_gen_models[].model** | 算法标识，默认 `jimeng_t2i_v31` | 否 |
| **image_gen_models[].base_url** | API 端点，默认 `https://visual.volcengineapi.com` | 否 |

示例：

```json
{
  "image_gen_models": [
    {
      "name": "jimeng",
      "provider": "jimeng",
      "api_key": "AKLTxxx",
      "secret_key": "xxx",
      "model": "jimeng_t2i_v31"
    }
  ]
}
```

配置后 Agent 可调用 `image_gen` 工具生成图片，自动下载保存到当前目录。

### 项目约定

NekoCode 启动时自动发现并加载项目约定文件，按优先级从低到高：

1. **用户全局**：`~/.nekocode/NEKOCODE.md` — 对所有项目生效的个人偏好
2. **项目根**：`<project>/NEKOCODE.md` — 项目级约定
3. **项目隐藏**：`<project>/.nekocode/NEKOCODE.md` — 项目级补充约定
4. **规则目录**：`<project>/.nekocode/rules/*.md` — 按文件名排序加载

支持 `@include` 指令递归加载其他文件（最大深度 3 层）：

```markdown
# 项目约定
- 使用 Go 1.25+
- 所有导出函数必须有注释
- 测试覆盖率 > 80%

@include docs/coding-style.md
```

---

## 路线图

### 已完成

- **Agent 核心**：Reason → Execute → Feedback 三轮循环，并行工具调度，子 Agent 委派，Mid-run 中断纠正
- **Agent Governance**：ToolSemanticClassifier 语义分类、AgentLedger 账本追踪、FinalCheck 最终校验、PreToolUse BlockTool / PostTurn BlockFinal 策略门禁
- **上下文管理**：五层压缩流水线 + 五级预警水位 + 锚点保留 + 摘要二次验证
- **防幻觉纵深防御**：Hook 引擎（8 内置 Hook + 7 事件点）、Token 预算管理、探索配额
- **多 Provider 网关**：Anthropic + OpenAI 协议统一接入，Prompt Caching，Thinking 跨 Provider 互译，运行时切换
- **Skill + Plugin + MCP**：技能包（YAML 定义 + 内置 bundled skill）、Plugin 生态（Claude Code 兼容）、MCP 外部工具扩展
- **项目感知**：NEKOCODE.md 自动发现 + @include 递归加载，CIndex 代码索引（Tree-sitter 多语言 + 图遍历 + FTS5 搜索 + SQLite 持久化），AgentMD 子代理定义
- **会话记忆**：结构化笔记异步提取，跨对话复用，Session 存档恢复
- **TUI 交互**：厚色条角色配色、工具卡片折叠展开、diff 高亮内联、思考分区、斜杠命令补全
- **文生图**：即梦 Jimeng t2i_v31 接入、火山引擎 SigV4 签名、自动下载保存本地
- **工程基础**：全局调试日志、文件缓存（LRU + mtime）、HTML→Markdown 转换、BotInterface 精简（12 方法）、纯 Go SQLite（零 CGO）

### 计划中

- **Checkpoint / Undo**：每次写入前自动快照，随时回滚
- **凭证管理**：多 profile 安全切换，开发/生产环境隔离
- **后台任务 + 进度**：长任务异步执行，进度实时展示
- **自动化测试**：Agent 行为回归测试（mock LLM 响应）


## 文档

- [架构文档](docs/ARCHITECTURE.md) — Agent 循环 · 数据流 · 上下文管理 · 架构决策 · 模块解耦
- [设计文档](docs/DESIGN.md) — 交互设计 · 视觉方案 · 防幻觉
- [开发路线](docs/PLAN.md) — 已完成 & 计划中 · 实施状态
- [Agent 治理层](docs/AGENT_GOVERNANCE.md) — 语义分类 · 账本追踪 · 最终校验 · 策略门禁
- [功能缺失分析](docs/FEATURE_GAP.md) — 与 Claude Code 对比 · 已实现 & 缺失功能
- [Bot 重构方案](docs/BOT_REFACTOR.md) — 模块重组 · 职责拆分 · 依赖方向


## License

MIT License
