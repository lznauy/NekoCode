# NekoCode

<p align="center">
  <b>你的猫娘 AI 编程助手 &nbsp;·&nbsp; MIT 开源 &nbsp;·&nbsp; 一个文件就能跑</b>
</p>

<p align="center">
  <sub>子任务并行 · 长会话恢复 · 检查点回滚 · 终端 / 编辑器 / IM 多端接入</sub>
</p>

<p align="center">
  <img src="docs/images/demo.gif" width="90%" alt="NekoCode 演示">
</p>

<p align="center">
  <b>简体中文</b> · <a href="docs/README.en.md">English</a>
</p>

---

## 这是什么？

NekoCode 是一个开源的 **AI 编程工作台**：不仅能读代码、改文件、跑测试，还能把较大的任务拆成可持续跟进的子任务，并通过终端、编辑器、IM 或你自己的应用完成工作。

### 不止能聊，还能把任务做完

- **并行子任务**：主 Agent 可以委派多个子 Agent；任务独立保存上下文、轮次和工具输出，支持排队、查看过程以及通过 `resume_id` 继续执行
- **任务过程看得见**：按 `Ctrl+G` 或输入 `/tasks` 打开任务中心，查看运行状态、每一轮结果和大输出，不必让工具流水淹没主对话
- **长会话也能继续**：完整历史追加保存并按索引分页；活动上下文支持自动或手动压缩，重新打开会话时无需把全部历史一次性载入内存
- **改错了可以回退**：文件修改自动建立检查点，通过 `/rewind` 按用户消息恢复文件状态
- **执行过程可控制**：权限审批、沙箱、Plan Mode、任务中断与运行中补充指示，让自动化保留明确的人工控制点

内置文件、Shell、代码索引、Web 搜索与网页抓取工具，也支持图片理解和通过已配置模型生成图片，可以覆盖从项目调研到修改验证的完整流程。任务中心目前由 TUI 提供并默认显示当前父会话；内存等待队列不跨进程恢复，详见[子任务使用说明](docs/USER_GUIDE.md#查看子任务)。

### 一个底座，多种工作方式

| 使用场景 | NekoCode 能力 |
|---|---|
| 在终端日常开发 | Bubble Tea TUI、流式输出、历史分页、任务中心 |
| 离开电脑继续派任务 | Telegram、飞书、QQ、企业微信连接器 |
| 接入编辑器 | 稳定版 Agent Client Protocol（ACP）v1 |
| 接入脚本或自有产品 | Headless NDJSON、Runtime Go API、HTTP/SSE |
| 扩展工具与工作流 | Skill、Plugin、MCP、Hooks，以及 Claude Code 风格扩展的兼容子集 |

### 模型和数据不由单一厂商锁定

可以通过已实现的 OpenAI 或 Anthropic 协议能力接入 Anthropic、DeepSeek、Kimi、GLM 等服务，并用 `/model` 切换。会话、检查点和运行记录保存在本地 `~/.nekocode`，目录格式公开，便于备份、审计和二次开发。

TUI 以单个可执行文件发布；Linux 版本需要兼容的 glibc 环境。项目采用 `bot → runtime → interaction` 分层，同一个 Agent 底座可以嵌入不同应用。


## 快速开始

### 第一步：一键安装

```bash
curl --proto '=https' --tlsv1.2 -fsSL https://raw.githubusercontent.com/lznauy/NekoCode/master/scripts/install.sh | sh
```

> 脚本自动识别系统（Linux / macOS）和架构，安装到 `~/.local/bin`。
> 脚本会用 Release 中的 `SHA256SUMS` 校验下载的二进制；v0.4.2 使用安装器内置的已核验 SHA-256。
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

ACP 使用 stdio 通信，因此该模式不会启动 TUI。会话配置仅在当前 ACP 连接内生效，不会覆盖用户的全局配置。出于安全考虑，客户端提供的 stdio MCP 进程默认禁用；仅在完全信任客户端及其工作区配置时使用 `nekocode-tui --acp --allow-client-mcp`。能力清单与限制见 [docs/ACP.md](docs/ACP.md)。

### 作为 Stream JSON 子进程使用

```bash
nekocode-tui -p "解释这个项目" --output-format stream-json
nekocode-tui --headless
```

使用自有 `nekocode-headless/2` 协议，支持 NDJSON 多轮输入、工具与子代理事件、审批、
会话管理、模型选择、检查点回滚和运行控制。接入开发指南见 [headless/README.md](headless/README.md)，
完整消息契约见 [STREAM_JSON.md](docs/STREAM_JSON.md)。

## 了解更多

- 详细使用、配置指南，可以参考文档 [用户使用指南](docs/USER_GUIDE.md)
- 桌面窗口版（GUI）暂未随 Release 发布，目前尚不完善，还在开发中

NekoCode 仍处于测试阶段，尚未经过大规模生产验证。工具设计、前缀缓存、上下文压缩和治理策略记录在开发者文档中。

## 给开发者

- [ACP.md](docs/ACP.md)：ACP v1 协议、能力协商、方法清单和 NekoCode 实现状态
- [ARCHITECTURE.md](docs/ARCHITECTURE.md)：整体架构、Agent 循环、工具系统、技术细节
- [DATA_DIRECTORY.md](docs/DATA_DIRECTORY.md)：`~/.nekocode` 目录架构、数据流与可删除性
- [SUBAGENT_EVOLUTION_PLAN.md](docs/SUBAGENT_EVOLUTION_PLAN.md)：子任务后续优化草案、分阶段计划与风险评估
- [IMAGES.md](docs/IMAGES.md)：各接入面的图片收发能力、marker 约定与模型消费路径
- [CI_CD.md](docs/CI_CD.md)：持续集成、版本发布、产物校验与失败处理
- [RUNTIME_APP_GUIDE.md](docs/RUNTIME_APP_GUIDE.md)：基于底座组装上层 AI 应用
- [RUNTIME_HTTP_API.md](docs/RUNTIME_HTTP_API.md)：Runtime HTTP/SSE 协议
- [CHANGELOG.md](docs/CHANGELOG.md)：版本变化记录

## 开源

- [贡献指南](docs/CONTRIBUTING.md)
- [行为准则](docs/CODE_OF_CONDUCT.md)
- [项目治理](docs/GOVERNANCE.md)
- [安全策略](docs/SECURITY.md)
- [支持范围](docs/SUPPORT.md)

NekoCode 使用 [MIT License](LICENSE)，可以按许可证条款使用、修改和分发。


## Star 趋势

<a href="https://www.star-history.com/?repos=lznauy%2FNekoCode&type=date&legend=top-left">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/lznauy/NekoCode/star-history/assets/star-history/star-history-dark.svg" />
   <source media="(prefers-color-scheme: light)" srcset="https://raw.githubusercontent.com/lznauy/NekoCode/star-history/assets/star-history/star-history-light.svg" />
   <img alt="NekoCode Star 趋势图" src="https://raw.githubusercontent.com/lznauy/NekoCode/star-history/assets/star-history/star-history-light.svg" />
 </picture>
</a>
