<!--
    /\___/\
   ( ◉   ◉ )   NekoCode
    =  ▾  =
   /|     |\
  (_|     |_)
     || ||
-->

<p align="center">
  <br>
  <img src="" width="0" height="0">
</p>

# NekoCode

<p align="center">
  <b>终端里的 AI 伙伴，不止于终端</b><br>
  <sub>开源 · 多模型自由 · 猫娘角色 · Go 单二进制 · 可嵌入的 Agent 核心</sub>
</p>

<p align="center">
  <sub>Anthropic / OpenAI / GLM / DeepSeek · Agent 循环 · 子 Agent 委派 · 上下文管理 · 会话记忆</sub>
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

MIT 开源，代码完全透明。Anthropic、OpenAI、GLM、DeepSeek 统一网关接入，一个工具切换所有模型。今天用 AI 写代码，明天用 GLM 做中文创意——改一行配置的事。

**终端也可以好看**

厚左色条角色配色、工具卡片折叠展开、diff 高亮内联、思考过程实时分区展示——每个交互细节都经过打磨。终端不是妥协，是选择。

**纵深防御幻觉**

从 System Prompt 约束、运行时强制校验（先读后改、二进制检测）、Hook 实时 hint 注入（重复调用检测、探索枯竭预警）、断路器（连续乱码终止）、独立验证 agent、回环编辑指纹检测、记忆漂移防护、来源引用强制、上下文保真压缩，到思考模式自适应控制——每一层独立生效，层层兜底。

**越聊越懂你**

长对话自动提取结构化笔记——目标、进度、关键决策、下一步行动——写入本地。开新对话时自动注入，不消耗 API token，助理永远记得上次聊到哪了。

**不止于终端**

Bot 核心通过接口与 UI 完全解耦。同样的 Agent，今天跑在终端 TUI 里，明天可以接入 Web GUI、桌面应用、甚至 IM 消息平台——逻辑不改，只换壳。

---

## 功能

| | | | |
|:--|:--|:--|:--|
| **对话** | 自然语言交互 · 猫娘角色 | **Shell** | 命令执行 · 4 级安全分级 |
| **文件** | 读取 · 写入 · 精确编辑 + diff | **搜索** | glob 模式 · ripgrep 内容搜索 · 网页搜索 |
| **子 Agent** | 5 种类型独立委派 | **记忆** | 长对话自动压缩 · 会话记忆复用 |
| **确认** | 写入/危险操作弹框确认 | **命令** | `/` 斜杠命令 · 实时补全 |
| **折叠** | 工具组折叠 · diff 展开 | **多模型** | Anthropic / OpenAI / GLM / DeepSeek |
| **Skill** | 可安装技能包 · 社区共享 | **上下文** | 五层压缩 · 四级预警 · 锚点保留 |
| **Hook** | 实时 hint 注入 · 断路器 | **预算** | 探索分数 · 三档配额水位 |

---

## 命令

| 命令 | |
|------|------|
| `/help` | 显示命令列表 |
| `/new` | 新对话（保留会话记忆） |
| `/clear` | 清空所有历史 |
| `/stats` | 上下文用量统计 |
| `/summarize` | 手动压缩记忆 |
| `/config` | 当前 provider / model |
| `/plan <任务>` | 只读探索模式，设计方案后审批执行 |
| `/<skill>` | 加载技能工作流 |

输入 `/` 自动弹出补全，Tab 选择，Enter 填入。

---

## 安全分级

| 等级 | 行为 | 示例 |
|:--|:--|:--|
| `safe` | 自动放行，无需确认 | `read` `glob` `grep` `ls` `git log` |
| `modify` | 弹框确认 | `write` `edit` `bash` `mkdir` |
| `danger` | 红色警告确认 | `rm` `kill` `git push -f` |
| `forbidden` | 直接拒绝 | `sudo` `curl\|bash` `ssh` `dd` |

bash 命令智能识别——`go build`、`git diff` 等纯输出命令自动降级为 safe，不用每次确认。

---

## 架构理念

```
┌──────────────────────────────────────┐
│              TUI / GUI / IM          │  ← 任意前端，通过接口对接
│         BotInterface (17 methods)    │
├──────────────────────────────────────┤
│           Bot Core (独立进程)        │
│  ┌──────────┐  ┌──────────────────┐  │
│  │ Agent循环│  │  上下文管理器    │  │
│  │ Reason→  │  │  五层压缩+锚点   │  │
│  │ Execute→ │  │  四级预警水位    │  │
│  │ Feedback │  └──────────────────┘  │
│  └──────────┘  ┌──────────────────┐  │
│  ┌──────────┐  │  会话记忆        │  │
│  │ 子 Agent │  └──────────────────┘  │
│  │ 5 种类型 │  ┌──────────────────┐  │
│  └──────────┘  │  工具系统 (13)   │  │
│  ┌──────────┐  └──────────────────┘  │
│  │ Hook引擎 │  ┌──────────────────┐  │
│  │ 9+1 钩子 │  │  Token 预算管理  │  │
│  └──────────┘  └──────────────────┘  │
│  ┌──────────┐  ┌──────────────────┐  │
│  │ Skill引擎│  │  项目上下文      │  │
│  │ 技能     │  │  NEKOCODE.md     │  │
│  └──────────┘  └──────────────────┘  │
├──────────────────────────────────────┤
│          LLM 统一网关                │
│  Anthropic / OpenAI / GLM / DeepSeek │
└──────────────────────────────────────┘
```

Bot 核心不依赖任何特定 UI 框架。`BotInterface` 定义了完整的 Agent 交互契约——发送消息、流式回调、工具确认、中止控制。换个前端只需实现这个接口。

---

## 快速开始

```bash
mkdir -p ~/.nekocode
cat > ~/.nekocode/config.json << 'EOF'
{
  "provider": "anthropic",
  "api_key": "sk-your-key-here",
  "model": "claude-sonnet-4-6",
  "base_url": "https://api.anthropic.com/v1",
  "token_budget": 128000,
  "thinking_budget": 16000,
  "max_iterations": 30
}
EOF

go build -o nekocode .

# 交互模式
./nekocode

# 单次调用
./nekocode "帮我看看 main.go 的内容"
```

---

## 路线图

### 已完成

- **Agent 循环**：Reason → Execute → Feedback 三轮循环，并行工具调度，子 Agent 委派
- **13 个内置工具**：bash、文件读写编辑、glob/grep 搜索、网页搜索/抓取、任务跟踪、子 agent 委派、项目索引
- **多 Provider 网关**：Anthropic + OpenAI + GLM + DeepSeek 统一接入
- **五层压缩流水线**：Tool Result Budgeting → History Sniping → MicroCompact → Collapsing → Full Compact 递进压缩
- **四级预警水位**：Normal → Warning → MicroCompact → Compact → Blocking，按 token 预算动态缩放
- **上下文锚点**：压缩时自动保留关键用户指令和系统约束
- **摘要验证**：LLM 生成的摘要经二次校验后写入，防止关键信息丢失
- **文件缓存**：LRU + mtime 去重，跨子 Agent 共享，避免重复读取
- **会话记忆**：异步提取，跨对话复用
- **Hook 机制**：9 种 InjectHook 实时注入 hint 提示（配额预警、探索枯竭、重复调用检测等）+ 1 种 StopHook 断路器
- **Token 预算管理**：ExplorationTracker 探索分数 + ToolQuota 三档水位（绿/黄/红），配额扩展 + 事务回滚
- **回环编辑检测**：Go 文件 GenDecl 指纹检测 A→B→A 无效编辑欺诈
- **Skill 系统**：可安装技能包，YAML 定义工作流，社区共享
- **项目感知**：自动发现 NEKOCODE.md，@include 递归加载项目约定
- **Mid-run 中断**：处理中随时纠正方向
- **指数退避重试**：LLM 调用自动恢复，4 次尝试 500ms→4s
- **TUI 组件化**：厚色条、工具卡片、diff 折叠、思考分区
- **Plan 模式**：`/plan` 只读探索，设计方案后审批执行
- **Session 管理**：对话存档恢复，支持分支对话

### 进行中

- **后台任务**：长命令流式输出，不阻塞主循环
- **MCP 协议支持**：连接外部 MCP server，工具生态无限扩展。数据库查询、K8s 管理、监控告警——任何 MCP server 都是 NekoCode 的工具
- **Web GUI**：Bot 核心通过接口解耦，Web 前端无缝对接。同一个 Agent，浏览器里用
- **IM 接入**：对接企业微信、飞书、Slack，Bot 作为全天候托管 Agent。早上收到任务，晚上回来验收——全程在 IM 里完成

### 计划中

- **Checkpoint / Undo**：每次写入前自动快照，随时回滚
- **凭证管理**：多 profile 安全切换，开发/生产环境隔离
- **BoltDB 持久化**：对话历史持久存储，重启不丢失

---

## 文档

- [架构文档](docs/ARCHITECTURE.md) — Agent 循环 · 数据流 · 上下文管理
- [架构设计](docs/ARCH_DESIGN.md) — 架构决策 · 模块解耦
- [设计文档](docs/DESIGN.md) — 交互设计 · 视觉方案 · 防幻觉
- [开发路线](docs/PLAN.md) — 已完成 & 计划中
- [上下文管理](docs/CONTEXT_DESIGH.md) — 压缩分层 · Token 追踪 · 缓存体系
- [压缩设计](docs/COMPACT_DESIGN.md) — 微压缩 · 摘要验证 · 锚点保留
- [Token 编排](docs/TOKEN_ORCHESTRATION.md) — 预算分配 · 动态调度

---

## License

本项目使用MIT协议，欢迎大家学习参考～
