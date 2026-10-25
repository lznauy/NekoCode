# NekoCode

<p align="center">
  <b>你的猫娘 AI 编程助手 &nbsp;·&nbsp; MIT 开源 &nbsp;·&nbsp; 一个文件就能跑</b>
</p>

<p align="center">
  <sub>顺手的终端交互 · 缓存复用省成本 · 并行子任务 · 长会话恢复 · 按需扩展</sub>
</p>

<p align="center">
  <img src="docs/images/demo.gif" width="90%" alt="NekoCode 演示">
</p>

<p align="center">
  <b>简体中文</b> · <a href="README.en.md">English</a>
</p>

---

NekoCode 是一个开源的 **AI 编程工作台**。你可以直接描述需求，让它阅读项目、修改代码、运行命令和验证结果；较大的任务可以交给多个子 Agent 并行推进，过程、用量和文件改动都能查看。

日常使用从一个终端程序和一份模型配置开始。需要更多能力时，可以逐步加入 Skill、Plugin、MCP 和 Hook，也可以把同一个 Agent 底座接入编辑器、IM 或自己的应用。

## 为什么好用

### 交互顺手，过程看得清

TUI 支持流式回答、Markdown 展示、命令补全和菜单选择。运行状态、工具活动、文件改动和任务进度集中展示，审批与提问在当前界面处理。上翻阅读历史时保留位置，按 `End` 回到最新内容；需要调整方向时，可以中断或在运行中补充指示。

按 `Ctrl+G` 或输入 `/tasks` 打开子任务中心，查看执行状态、历史轮次、最终结果和可分页的大输出。主对话保留进度摘要，详细过程按需展开，多个任务同时运行时也便于跟进。

### 安装轻，配置从模型开始

TUI 以单个可执行文件发布，基础使用无需部署前端或数据库服务。安装后填入 API Key、模型 ID 和服务地址即可开始，模型切换、会话选择、权限和扩展管理都有命令入口。

项目规范写进 `NEKOCODE.md`，项目与用户级扩展分开管理。同名 Skill 按明确优先级选择，项目定制可以随项目使用。MCP 服务、语言服务器和图片模型按需要另行配置。

### 长会话按需加载，控制内存增长

当前上下文与完整对话历史分开保存：历史追加落盘，查看时按索引分页，恢复会话时读取当前所需上下文。运行记录和历史缓存设有预算，大工具输出通过文件引用和分页读取，减少长期使用中反复把全部历史装进内存的压力。

自动摘要和 `/compact` 缩减发送给模型的活动上下文，完整历史仍可查看和导出。实际内存占用取决于上下文规模、启用的扩展和外部工具进程；这些机制侧重控制长会话的资源增长。

### 提示词集中，工作流按需加载

[核心提示词](bot/prompt/system_zh.md) 集中定义通用协作规则，便于阅读和修改；项目约定通过 `NEKOCODE.md` 提供。排障、规划、审查等专门流程放在 Skill 中，需要时才加载完整正文，也可以用 `$技能名` 明确指定。

### 围绕高缓存命中设计，节省输入成本

多轮编程任务会反复携带项目指令、工具定义和历史上下文，这部分往往占据输入的大头。NekoCode 将稳定指令与动态运行信息分开组装，把易变内容放在消息流尾部，保持技能目录和工具定义的稳定；技能名单在会话内冻结，中途加载技能不会让已有前缀失效。MCP 通过固定结构的代理工具接入，减少服务上下线引起的工具列表变化，让相同请求前缀更容易被模型服务复用。

在支持缓存优惠的模型服务上，命中的输入 token 按缓存价格计费，任务轮次越多、上下文越长，省下的重复输入费用越多。`/context` 和 TUI 顶栏展示整场会话及最近一轮的缓存命中率与用量，命中偏低时还会给出异常归因，方便检查实际效果；命中情况和节省幅度取决于服务商、模型、缓存有效期及请求内容。

### 内置代码索引，查符号和调用关系不靠猜

`index` 工具基于内置的 tree-sitter 解析和本地 SQLite 索引，覆盖 Go、JavaScript/JSX、TypeScript/TSX、Python 和 Rust：可以查符号定义、谁在调用它、它调用了谁、包依赖和全文检索。索引在打开项目时构建并持久化到 `.nekocode/index.db`，之后由文件监听按内容哈希增量更新；启动时还会把项目骨架注入提示词，让模型先有整体结构再动手。

这些能力都在同一个二进制里，不需要额外启动语言服务或外部索引 MCP；需要类型级跳转时可以再用 LSP 辅助。适用的语言范围和索引结果以实际解析能力为准。

### 沙箱随二进制一起交付，环境不满足时自动降级

Shell 命令默认在隔离的 `workspace-write` 沙箱中运行：主机文件系统只读可见，工作区和获批目录可写；也可以按命令或权限规则选用 `read-only`，或在明确授权后改用 `host`。网络访问和额外写目录作为能力单独授予，与命令审批合并展示，不会连续追问。

Linux 优先使用内置的用户命名空间与挂载隔离，不依赖 bubblewrap 等外部组件；该路径不可用时自动退回 Landlock，并显式放行 `/nix/store` 等运行依赖，兼顾 NixOS 和受限容器。具体的隔离强度取决于内核能力和所选后端。

### 同一个底座，既能进终端也能进 IM

TUI、编辑器 ACP、Headless、HTTP/SSE 和 Telegram、飞书、QQ、企业微信共用同一套 Agent 能力，不是给 CLI 套一层转发脚本。IM 侧支持在会话中审批、回答提问、中断和补充指示，任务中心可以查看子任务；模型也能通过 `deliver` 工具主动把工作区产物发回当前渠道。讨论与执行可以在同一处完成，适合需要在手机上跟进长任务或公司内网协作的场景。

### 工具齐全，执行有控制点

从文件搜索、代码阅读、编辑，到 Shell、进程管理、网页调研和结果验证，常用工具已内置。Plan Mode 支持先审方案再动手，权限规则与沙箱控制执行范围，文件修改前检查读取依据，检查点支持按用户消息回退已记录的文件状态。

模型可以选择已实现的 OpenAI 或 Anthropic 协议能力接入服务，并通过 `/model` 切换。会话、检查点和运行记录保存在本机，方便备份、排查和接入自己的工具。

### 扩展和二次开发有明确入口

用 Skill 定义流程，用自定义 Agent 限定子任务的工具范围，用 Hook 响应生命周期事件，用 MCP 接入外部工具，再通过 Plugin 打包分发。现有 Claude Code 风格扩展可按已支持的兼容子集使用。

Go 开发者可以注册自己的工具、组装 Runtime、接入新 UI 或消息渠道。`bot → runtime → interaction` 分层让推理、运行管理和交互入口各自维护，公开协议与教程也随代码提供。

## 主要功能

| 功能 | 可以怎样用 |
| --- | --- |
| 文件与代码操作 | 文件及 PDF 读取、写入、精确编辑、差异预览、目录树和内容搜索；代码索引与 LSP 辅助定位，语言服务按环境配置 |
| Shell 与进程管理 | 运行构建、测试和开发命令，查看并管理工具启动的进程 |
| 网页调研 | Web 搜索、网页抓取和正文提取，结合本地代码处理任务 |
| 并行子 Agent | 独立工具范围和上下文，支持并发、排队、逐轮结果查看及续跑 |
| 任务清单与交互提问 | 跟踪待办与完成状态，在需要澄清时通过界面回答问题 |
| 长会话与项目规范 | 自动或手动摘要、历史分页、会话恢复、`NEKOCODE.md` 和项目记忆 |
| 文件回退与导出 | 检查点回退、对话导出、本地会话与运行记录 |
| 图片理解与生成 | 分析截图和图片；通过已配置模型生成图片，能力取决于模型和入口 |
| 结果文件交付 | 通过支持文件发送的 IM 渠道交付工作区产物 |
| 规划、审批与沙箱 | 先规划、按规则审批、限制工作区访问，并支持取消和追加指示 |
| 模型、缓存与用量 | 多模型配置、模型切换、推理强度设置、稳定前缀复用，以及 token 和缓存命中统计 |
| 扩展系统 | Skill、Plugin、自定义 Agent、MCP 和 Hook；可选 Jev 辅助判断与摘要相关性预筛 |
| 代码索引 | 内置 tree-sitter 索引：符号、调用关系、包依赖和全文搜索，按文件变更增量更新 |
| 多端交互 | 终端 TUI、编辑器 ACP、Headless、HTTP/SSE、Telegram、飞书、QQ 和企业微信 |
| 嵌入与观测 | Runtime Go API、自定义工具和渠道接入，结构化运行事件、诊断日志与 Hook 只读检查 |

## 选择你的使用方式

| 场景 | 入口 |
| --- | --- |
| 在终端开发 | `nekocode-tui`，键盘操作、流式输出和任务中心 |
| 在编辑器工作 | ACP v1，通过 stdio 连接支持该协议的客户端 |
| 通过 IM 派发和跟进任务 | Telegram、飞书、QQ、企业微信连接器，按平台支持的能力交互 |
| 脚本或子进程调用 | Headless 双向 NDJSON 协议，支持审批、提问、会话与运行控制 |
| 自有应用、UI 或服务 | Runtime Go API 与 HTTP/SSE |
| 桌面窗口 | GUI 正在开发，暂未随 Release 发布 |

## 快速开始

### 第一步：一键安装

```bash
curl --proto '=https' --tlsv1.2 -fsSL https://raw.githubusercontent.com/lznauy/NekoCode/master/scripts/install.sh | sh
```

> 脚本自动识别系统（Linux / macOS）和架构，安装到 `~/.local/bin`。
> 脚本会用 Release 中的 `SHA256SUMS` 校验下载的二进制；v0.4.2 没有对应的校验文件，使用安装器内置的已核验 SHA-256。
> Windows 用户请通过 [WSL](https://learn.microsoft.com/windows/wsl/install) 运行。

<details>
<summary>其他安装方式（手动下载 / 源码编译）</summary>

从 [Releases 页面](https://github.com/lznauy/NekoCode/releases) 下载对应平台的二进制，加执行权限直接运行。

或源码编译（需要 [Go 1.26.10+](https://go.dev/dl/)）：

```bash
git clone https://github.com/lznauy/NekoCode.git
cd NekoCode
go build -o nekocode-tui ./cmd/tui
```

</details>

### 第二步：配置 API Key

```bash
mkdir -p ~/.nekocode
cat > ~/.nekocode/config.json << 'EOF'
{
  "active": "deepseek",
  "models": [
    {
      "name": "deepseek",
      "provider": "deepseek",
      "api_key": "sk-你的Key写这里",
      "model": "deepseek-v4-flash",
      "base_url": "https://api.deepseek.com/v1",
      "protocol": "openai"
    }
  ]
}
EOF
```

### 第三步：开聊

```bash
nekocode-tui
```

配置完成后即可开始使用。常用入口：`/plan <任务>` 先审方案、`/tasks` 查看子任务、`/compact` 压缩上下文、`/rewind` 回退文件改动、`/sessions` 恢复会话、`/context` 查看上下文和缓存用量。

### 作为 ACP Agent 使用

NekoCode 支持稳定版 Agent Client Protocol v1。让编辑器以项目目录为工作目录启动：

```bash
nekocode-tui --acp
```

ACP 使用 stdio 通信，因此该模式不会启动 TUI。会话配置仅在当前 ACP 连接内生效，不会覆盖用户的全局配置。出于安全考虑，客户端提供的 stdio MCP 进程默认禁用；仅在完全信任客户端及其工作区配置时使用 `nekocode-tui --acp --allow-client-mcp`。能力清单与限制见 [docs/developers/ACP.md](docs/developers/ACP.md)。

### 作为 Stream JSON 子进程使用

```bash
nekocode-tui -p "解释这个项目" --output-format stream-json
nekocode-tui --headless
```

使用自有 `nekocode-headless/2` 协议，支持 NDJSON 多轮输入、工具与子代理事件、审批、
会话管理、模型选择、检查点回滚和运行控制。接入开发指南见 [headless/README.md](headless/README.md)，
完整消息契约见 [STREAM_JSON.md](docs/developers/STREAM_JSON.md)。

## 文档与教程

从 [文档目录](docs/README.md) 按角色选择入口：

| 读者 | 建议阅读 |
| --- | --- |
| 使用者 | [安装与使用](docs/users/USER_GUIDE.md)、[故障排查](docs/users/TROUBLESHOOTING.md)、[会话恢复与备份](docs/users/SESSION_GUIDE.md)、[版本变化](docs/users/CHANGELOG.md) |
| 扩展作者 | [Skill](docs/developers/SKILL_GUIDE.md)、[自定义 Agent](docs/developers/AGENT_GUIDE.md)、[Plugin](docs/developers/PLUGIN_GUIDE.md)、[Hook](docs/developers/HOOK_GUIDE.md) |
| 二次开发者 | [工具开发](docs/developers/TOOL_GUIDE.md)、[Runtime 应用](docs/developers/RUNTIME_APP_GUIDE.md)、[HTTP API](docs/developers/RUNTIME_HTTP_API.md)、[Headless 接入](headless/README.md) |
| 项目开发维护者 | [贡献指南](docs/policy/CONTRIBUTING.md)、[架构](docs/maintainers/ARCHITECTURE.md)、[架构边界](docs/maintainers/ARCHITECTURE_BOUNDARIES.md)、[CI/CD](docs/maintainers/CI_CD.md)、[官方发布](docs/maintainers/RELEASING.md) |

## 当前边界

Release 提供 Linux、macOS 的 amd64/arm64 TUI 二进制；Linux 需要兼容的 glibc 环境，Windows 通过 WSL 使用。GUI 仍在开发。项目处于持续迭代阶段，尚未经过大规模生产验证。

任务中心目前在 TUI 中提供，默认查看当前父会话的任务；已保存的任务可随会话查看和续跑，尚未开始的内存队列不会跨进程恢复。检查点覆盖受支持写入链路记录的文件，不是整个工作区或外部服务的事务回滚。

模型、MCP、图片和各接入端的能力以实际配置和已实现协议为准。数据位置与管理方式见 [数据目录](docs/users/DATA_DIRECTORY.md)，图片能力见 [图片支持](docs/users/IMAGES.md)；后续方向集中记录在 [规划目录](docs/plan/README.md)。

## 开源

- [贡献指南](docs/policy/CONTRIBUTING.md)
- [行为准则](docs/policy/CODE_OF_CONDUCT.md)
- [项目治理](docs/policy/GOVERNANCE.md)
- [安全策略](docs/policy/SECURITY.md)
- [支持范围](docs/policy/SUPPORT.md)

NekoCode 使用 [MIT License](LICENSE)，可以按许可证条款使用、修改和分发。


## Star 趋势

<a href="https://www.star-history.com/?repos=lznauy%2FNekoCode&type=date&legend=top-left">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/lznauy/NekoCode/star-history/assets/star-history/star-history-dark.svg" />
   <source media="(prefers-color-scheme: light)" srcset="https://raw.githubusercontent.com/lznauy/NekoCode/star-history/assets/star-history/star-history-light.svg" />
   <img alt="NekoCode Star 趋势图" src="https://raw.githubusercontent.com/lznauy/NekoCode/star-history/assets/star-history/star-history-light.svg" />
 </picture>
</a>
