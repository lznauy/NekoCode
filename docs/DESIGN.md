# NekoCode 设计文档

> **本文档职责**: 描述产品设计——UI 布局、交互模式、视觉主题、Agent 能力设计、上下文管理策略、防幻觉设计原则。不包含代码实现细节、文件路径、函数名等属于 ARCHITECTURE.md 的内容。更新时请保持此边界。

## 产品定位

NekoCode 是一个运行在终端中的 AI 助手。它能理解自然语言、执行本地操作（文件读写、Shell 命令、文件搜索），并在执行可能有影响的操作前征求用户确认。

核心体验：**像和一位终端里的伙伴聊天一样，自然地交代任务，它帮你完成。**

## 交互模式

用户说"你好"等纯对话时，助手自然语言回复，不触发工具；说"帮我看看 main.go"等操作请求时，自动选择合适工具执行。**用户无需手动切换模式**——助手内部自动判断该聊天还是该操作。

### 斜杠命令

以 `/` 开头的输入为系统命令：

| 命令 | 效果 |
|------|------|
| `/help` | 显示可用命令列表 |
| `/new` | 开始新对话（保留上一任务摘要） |
| `/clear` | 清空所有对话历史和摘要 |
| `/stats` | 查看上下文状态：消息数、tokens、是否有摘要 |
| `/summarize` | 手动触发上下文压缩，返回压缩前后对比 |
| `/config` | 显示当前 provider 和 model |
| `/plan <任务>` | 进入只读探索模式，设计方案后审批执行 |
| `/<skill>` | 加载指定技能的工作流 |

## TUI 界面设计

### 视觉主题：深夜书房

黑猫蜷在屏幕旁的意象——teal 色偶尔闪现，像暗处的猫眼。

**色彩体系**（`tui/styles/colors.go` 统一定义）：
- 主文字：`#a0a0a0`
- Teal 主色：`#4ec9b0`（styles.Primary），用于 Assistant 色条、spinner
- User 金：`#c9a96e`（styles.Yellow）
- 蓝：`#7a8ba0`（styles.Blue）
- 红：`#e06c75`（styles.Red）
- Diff 绿：`#98c379`（styles.DiffGreen）
- 弱化文字：`#666666`，中间：`#808080`
- 边框线：`#333333`

### 启动页

```
          /\___/\
         ( ◉   ◉ )
          =  ▾  =
         /|     |\
        (_|     |_)
           || ||

         NEKOCODE
          v0.2.0

      ──── ◆ ────

         Press Enter
```

猫眼 `◉` 闪烁 teal 光。用户按下 Enter 进入聊天界面。

### 聊天界面布局（厚左色条）

```
(=^.^=) NEKOCODE v0.2.0

▐ You                                                        ┃
▐ 帮我分析下项目架构                                           ┃

▐ Assistant                                                  ┃
▐                                                            ┃
  ┌ ◆ read ×5 [+] 展开 ─────────────────────────────────┐     ┃
  │ ◆ grep "func" .  [+]                                │     ┃
  └──────────────────────────────────────────────────────┘     ┃
▐                                                            ┃
▐ ## 项目架构                                                 ┃
▐ ...                                                        ┃
▐ Duration: 12.3s  ↑670 ↓128                                 ┃
```

- **左侧**：`▐`（U+2590）厚色条 + `PaddingLeft(1)` 统一缩进
- **右侧**：独立 Scrollbar 组件，`┃` thumb + `│` track
- **工具卡片**：暖金色 `NormalBorder`，单次 edit 块显示 `[+]`/`[-]` 折叠展开 diff
- **edit 工具组**：`◆ edit ×3 [-] 收起` 展开后直接内联每个文件的 diff，`▍ path` 标注文件，一次展开全部可见
- **其他工具组**：同名单行工具折叠为 `◆ read ×5 [+]`，展开后逐条显示
- **处理卡片**：teal 边框，分隔线横跨全宽区分 output/reasoning 区块

### 处理阶段

```
▐ ◉ Thinking (3.2s) ↑670 ↓56 🧹3    ← 当前阶段 + 耗时 + token + 微压缩计数

▐   ▍ output ──────────────────────   ← 分隔线（teal）
▐   正在分析项目结构...                ← 模型流式输出（动态 2-6 行）

▐   ▍ reasoning ───────────────────   ← 分隔线（蓝色）
▐   让我读取所有源文件来分析...        ← 推理过程（动态 2-6 行）

▐   ◆ glob ×2 [-] 收起                ← 收折工具组
▐     ◆ glob *.go                     ← 展开：逐条显示
▐     ◆ glob *.md
```

阶段流转：Waiting → Thinking → Reasoning → Running → Thinking → ... → Ready

- **Waiting**: LLM 调用已发出，等待首 token
- **Thinking**: ReasoningContent 到达（模型 CoT 推理）
- **Reasoning**: Content token 到达，模型生成文本中
- **Running**: 工具执行中
- **🧹N**: 累计微压缩清除的工具结果数

### 工具组折叠

```
◆ read ×15 [+] 展开    ← 收起（单行）
◆ read ×15 [-] 收起    ← 展开逐条：
  ◆ read (1/15) /path/to/file1.go
  ◆ read (2/15) /path/to/file2.go

◆ edit ×3 [-] 收起     ← edit 组展开，diff 内联
  ▍ server/main.go
    ── diff ──
    - old code
    + new code
  ▍ server/game.go
    ── diff ──
    - old line
    + new line
```

### 工具确认栏

```
Confirm
  bash go test ./...  [safe]
  Proceed?  [enter] yes  [esc] no
```

- 展示具体命令/路径而非仅工具名（如 `bash go build`、`write server/main.go`）
- 等级标签：`[safe]`/`[modify]`/`[danger]`/`[blocked]`
- `[modify]`/`[danger]` 黄色，`[blocked]` 红色（直接拒绝不弹框）
- `[safe]` 命令自动放行，不弹确认框

### 输入交互

- **发送**：Enter 提交，消息即时显示
- **处理中输入（BTW）**：Enter 注入新消息打断当前 LLM 调用
- **历史翻阅**：↑/↓ 翻阅历史
- **命令提示**：输入 `/` 弹出命令列表，Tab/Shift+Tab 选择
- **块切换**：Ctrl+E 展开/收起工具组和 edit diff

## 上下文管理

### 五级策略

| 层 | 触发条件（buffer 剩余） | 动作 |
|----|---------|------|
| **Normal** | > 44,800 | 无操作 |
| **Warning** | ≤ 44,800 | 仅告警，不操作 |
| **MicroCompact** | ≤ 35,200 | 清除旧 compactable 工具结果（read、grep、glob 等），保留最近 5 个 |
| **Compact** | ≤ 25,600 | LLM 生成结构化摘要压缩最旧消息 |
| **Blocking** | ≤ 6,400 | 拒绝新输入，强制压缩后继续 |

> 阈值针对 128K 上下文窗口自动缩放（DefaultConfig 基准为 64K）。


### 上下文锚点

压缩前自动标记应保留的关键消息——用户核心指令、系统约束、API 版本要求等。压缩过程中这些消息优先保留，防止关键上下文被误清除。

### 摘要验证

LLM 生成的摘要需要经过二次校验：检查是否保留了代码片段原文、错误信息原文、文件路径和行号等关键内容。验证失败则重新生成摘要，确保压缩保真度。

### Session Memory

上下文超过 10k token 后开始异步提取，每 +5k token + 3 个 tool call 再次触发。提取内容写入 `~/.nekocode/sessions/<id>/memory.md`（10 section Markdown 文件）。`/new` 命令优先用 session memory 作为免费摘要。

## Agent 能力

### 工具清单

| 工具 | 功能 | 安全等级 | 执行模式 |
|------|------|----------|----------|
| **bash** | Shell 命令（只读命令自动 Safe） | Safe～Forbidden | Sequential |
| **read** | 文件读取 + 二进制检测 + 文件未找到建议 | Safe | Parallel |
| **write** | 文件创建/覆盖（先读后改强制） | Write | Sequential |
| **edit** | 精确替换 + diff + 3轮模糊匹配 | Write | Sequential |
| **list** | 目录列表 | Safe | Parallel |
| **glob** | 文件模式匹配（支持 **） | Safe | Parallel |
| **grep** | ripgrep 内容搜索 | Safe | Parallel |
| **web_search** | Exa MCP 搜索 + 强制 Sources 引用 | Safe | Parallel |
| **web_fetch** | 网页抓取 + 125字符引述限制 | Safe | Parallel |
| **tree** | 目录树可视化 | Safe | Parallel |
| **project_info** | 项目符号/依赖/文件索引 | Safe | Parallel |

| **task** | 子 agent 委派 | Safe | Parallel |
| **todo_write** | 任务列表更新 | Safe | Sequential |

### 子 Agent 类型（3 种）

| 类型 | 用途 | 工具 |
|------|------|------|
| **executor** | 执行编码任务 | read/write/edit/bash/grep/glob/list |
| **verify** | 验证修改 | read/grep/glob/list/bash |
| **researcher** | 代码探索/调研 | read/grep/glob/list/web_search/web_fetch |

子 agent 通过独立 LLM 客户端运行（共享上下文窗口 128K、接入 Compactor），edit 操作需用户确认。Handoff 机制支持上下文传递。

### 危险命令分级

bash 命令按关键词智能分级，三层判断：

**降级至 Safe（自动放行）**：`go version`、`go vet`、`git status`、`git log`、`git diff`、`ls`、`cat`、`ps`、`du`、`file` 等纯输出命令
**升级至 Danger（危险，确认）**：`rm`、`chmod`、`kill`、`reboot`、`git push --force` 等
**升级至 Blocked（拒绝）**：`sudo`、`eval`、`ssh`、`curl|bash`、`dd`、`mkfs` 等
**默认 Modify（确认）**：其余所有命令

### 并行工具执行

互不依赖的工具并发执行，worker pool 上限 10。并行启动前检查 ctx 取消状态。subagent 共享同一个 Executor 实例。

## 幻觉防治

基于纵深防御思想，在 6 个代码层面实现多层防幻觉机制，辅以 prompt 级设计补充：

### 代码层

- **第 1 层 — 工具安全**: 危险等级四级分类、bash 命令智能分级、路径验证、二进制检测、URL 内网 IP 拒绝
- **第 2 层 — 执行拦截**: Forbidden 直接拒绝、Write+ 弹框确认、先读后改强制校验
- **第 3 层 — 输出完整性**: 工具结果边界标记、输出截断（2000行/50KB）、Garbled tool call 过滤
- **第 4 层 — Agent 循环控制**: 末日循环检测、收益递减检测、搜索断路器、finish_reason=length 处理
- **第 5 层 — 上下文保真**: 关键约束锚定、摘要二次验证、五级自动压缩、孤儿消息过滤
- **第 6 层 — LLM 调用控制**: 跨协议 thinking 开关控制、子 Agent thinking 强制关闭、token 超限时自动降级

### Prompt/设计级补充

- System Prompt 反幻觉指令（禁止生成 URL、忠实报告、推理长度限制）
- verify agent 格式强制 + 自检清单
- Session memory 模板警告（"记忆说 X 存在 ≠ X 现在存在"）
- web_search/fetch 的 Sources 引用格式要求
- edit 组内联展开（diff 一次可见，无需二次折叠）
- bash 复杂命令显示截断（只展示首行 + …）

### 跨目录编辑

编辑工具允许操作工作目录外的文件——`validatePath` 不再拒绝跨目录路径，确认系统负责用户同意。危险等级依据命令类型分级，而非路径位置。


### 设计原则

- **Ground Everything** — 每个决策锚定在可验证的现实中（文件系统、命令输出、URL 来源）
- **Assume Deception** — 任何 LLM 输出（包括子 agent）都可能包含幻觉，需独立验证
- **Make It Checkable** — 所有输出格式服务于可验证性（file_path:line_number、Sources、Command run）
- **Fail Loudly** — 幻觉不能被静默：先读后改违规 → 报错，末日循环 → 强制停止，二进制 → 明确拒绝
- **Budget Reasoning** — 推理有成本：按任务类型限制思考长度，禁止在未读代码前凭空分析
- **Self-Serve First** — 主 Agent 优先自己完成任务，子 agent 仅在满足三个条件（5+ 文件跨包 / 独立上下文 / 单回合确实太复杂）时才使用
- **Progressive Compression** — 上下文逐级压缩，不急丢信息：先微压缩后完整压缩，优先用 session memory 做免费摘要
- **Anchor & Verify** — 压缩时锚定关键信息，压缩后二次验证保真度，确保压缩不是"遗忘"
- **Know The Project** — 会话启动时自动发现 NEKOCODE.md，一次性预加载项目约定，后续所有对话受益

## 非交互模式

```bash
nekocode "帮我看看当前目录有什么文件"
```
