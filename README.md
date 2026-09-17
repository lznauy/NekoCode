# NekoCode

<p align="center">
  <b>你的猫娘 AI 编程助手 &nbsp;·&nbsp; MIT 开源 &nbsp;·&nbsp; 一个文件就能跑</b>
</p>

<p align="center">
  <sub>双界面 · 多模型自由切换 · 读代码 / 改文件 / 跑命令，全靠聊</sub>
</p>

<p align="center">
  <img src="docs/images/demo.gif" width="90%" alt="NekoCode 演示">
</p>

<p align="center">
  <b>简体中文</b> · <a href="docs/README.en.md">English</a>
</p>

---

## 这是什么？

NekoCode 目前的定位是 **AI 编程助手**，你只需要 **像聊天一样说出你的需求**，它就能帮你：

- **看懂项目**：快速理解陌生代码库，讲清楚某段逻辑在做什么
- **改代码**：加功能、修 bug、重构，并检查格式和语法
- **跑命令**：执行构建、测试等命令，持续返回运行状态
- **查资料**：联网搜索、抓取网页，补充需要实时确认的信息
- **生成图片**：通过已配置的图片模型生成示意图或封面图

**为什么选它？**

- **远程交互**：支持 Telegram、飞书、QQ 和企业微信，可在离开电脑时提交任务并接收结果
- **单二进制分发**：TUI 以单个可执行文件发布；Linux 版本需要兼容的 glibc 环境
- **Claude Code 风格扩展**：支持其插件和 Skill 格式的兼容子集，具体能力以文档和测试为准
- **二次开发**：`bot → runtime → interaction` 分层，可嵌入其他应用

它不锁定某一家模型厂商。Anthropic、DeepSeek、Kimi、GLM 等模型可以通过已实现的 OpenAI 或 Anthropic 协议能力接入。


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

或源码编译（需要 [Go 1.25.12+](https://go.dev/dl/)）：

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

配置完成后即可开始使用。

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

## Star 趋势

<a href="https://www.star-history.com/?repos=lznauy%2FNekoCode&type=date&legend=top-left">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/lznauy/NekoCode/star-history/assets/star-history/star-history-dark.svg" />
   <source media="(prefers-color-scheme: light)" srcset="https://raw.githubusercontent.com/lznauy/NekoCode/star-history/assets/star-history/star-history-light.svg" />
   <img alt="NekoCode Star 趋势图" src="https://raw.githubusercontent.com/lznauy/NekoCode/star-history/assets/star-history/star-history-light.svg" />
 </picture>
</a>

## 了解更多

- 详细使用、配置指南，可以参考文档 [用户使用指南](docs/USER_GUIDE.md)
- 桌面窗口版（GUI）暂未随 Release 发布，目前尚不完善，还在开发中

NekoCode 仍处于测试阶段，尚未经过大规模生产验证。工具设计、前缀缓存、上下文压缩和治理策略记录在开发者文档中。

## 给开发者

- [ACP.md](docs/ACP.md)：ACP v1 协议、能力协商、方法清单和 NekoCode 实现状态
- [ARCHITECTURE.md](docs/ARCHITECTURE.md)：整体架构、Agent 循环、工具系统、技术细节
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
