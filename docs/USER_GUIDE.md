# NekoCode 使用指南

本文档面向使用者，介绍如何安装、配置和日常使用 NekoCode，包括连接 Telegram / 飞书 / QQ / 企业微信机器人、安装技能（Skill）与插件（Plugin）、配置 MCP 服务等内容。

---

## 一、安装

### 一键安装（推荐）

```bash
curl --proto '=https' --tlsv1.2 -fsSL https://raw.githubusercontent.com/lznauy/NekoCode/master/scripts/install.sh | sh
```

脚本会自动识别系统（Linux / macOS）和架构，下载最新版本并安装到 `~/.local/bin`（无需 sudo）。安装完成后运行：

```bash
nekocode-tui
```

如果提示找不到命令，请把 `~/.local/bin` 加入你的 `PATH`。

其他安装方式：

- 指定版本：`curl --proto '=https' --tlsv1.2 -fsSL https://raw.githubusercontent.com/lznauy/NekoCode/master/scripts/install.sh | sh -s -- --version v0.5.0`
- 指定目录：`curl --proto '=https' --tlsv1.2 -fsSL https://raw.githubusercontent.com/lznauy/NekoCode/master/scripts/install.sh | sh -s -- --dir /你的/目录`
- 源码编译（需要 Go 1.25.12+）：`go build -o nekocode-tui ./cmd/tui`
- Windows 用户：请通过 WSL 运行

## 二、首次配置：接入模型

NekoCode 需要一个模型服务商的 API Key 才能工作。首次使用前，创建配置文件 `~/.nekocode/config.json`：

```json
{
  "active": "deepseek",
  "auto_compact_percent": 80,
  "models": [
    {
      "name": "deepseek",
      "provider": "deepseek",
      "api_key": "你的 API Key",
      "model": "deepseek-v4-flash",
      "base_url": "https://api.deepseek.com/v1",
      "protocol": "openai",
      "reasoning_effort": "medium"
    }
  ]
}
```

- `active`：默认使用的模型名（必须能在 `models` 列表里找到）
- `protocol`：填 `openai` 或 `anthropic`，绝大多数 OpenAI 兼容服务（DeepSeek、通义、Kimi 等）都填 `openai`
- 可以配置多个模型，用 `/model` 命令随时切换

配置好后运行 `nekocode-tui`，在输入框打字、回车，就开始对话了。

### 脚本与编辑器接入

配置完成后，可以在项目目录中使用无界面入口：

```bash
# 单次任务，输出 NDJSON，完成后退出
nekocode-tui -p "解释这个项目" --output-format stream-json

# 宿主通过 stdin/stdout 进行多轮交互
nekocode-tui --headless

# 支持 ACP 的编辑器
nekocode-tui --acp
```

Headless 使用自有 `nekocode-headless/2` 协议，支持工具审批、提问、会话管理和运行控制。
需要审批或回答问题时，宿主必须保持 stdin 打开并回复控制请求；stdin 关闭后交互自动拒绝。
接入示例见 [Headless 开发指南](../headless/README.md)，消息格式见
[Stream JSON 协议](STREAM_JSON.md)；编辑器能力与限制见 [ACP 文档](ACP.md)。

## 三、日常使用

### 基本操作

- **开始任务**：直接输入需求，回车发送。比如"帮我给这个项目加个登录接口"
- **停止任务**：任务运行中按 `Esc`
- **边跑边补充**：任务运行中可以继续输入，回车后作为追加指示插入当前任务
- **审批操作**：当 AI 要执行有风险的操作（运行命令、修改文件等）时，底部会弹出确认框：
  - **仅本次允许**：只放行当前这一次工具调用
  - **始终允许**：以后同类操作不再询问（规则保存在项目的 `.nekocode/permissions.json`)
  - **拒绝**：阻止本次操作
  - 命令本身和能预先判断的网络、写目录等沙箱能力会合并在同一张审批卡中，不会连续问两次；运行后才能发现的额外能力仍会单独询问
- **回答提问**：AI 需要你补充信息时会弹出选项框，方向键选择、空格勾选、回车提交

### 斜杠命令

在输入框输入 `/` 会弹出命令菜单。命令名用 Tab 或方向键选择；对于候选值有限的命令，输入完整命令后按 Enter 会继续展开选择菜单：

- `/model`：选择已配置模型
- `/effort`：选择当前模型的推理强度
- `/permission`：选择审批模式（manual / auto / full，auto 需配置 Jev，见「权限与安全」）
- `/rewind`：按用户消息选择最近 100 个恢复点，并显示相对位置、时间和文件变更数量；没有文件修改的消息也会保留为恢复锚点
- `/sessions`：选择历史会话；TUI 中可按 `d` 删除当前选中的会话（需二次确认）
- `/plugin`：先选择操作；enable/disable/info/uninstall 再选择插件
- `/connect`、`/disconnect`：选择连接器

菜单使用 `↑`/`↓` 或 Tab 移动，Enter 选择；叶子选项会直接执行，Esc 关闭菜单，在多级菜单中则返回上一级。模型、推理强度、权限等控制命令只更新系统状态和底部状态栏，不会显示为用户对话。自由文本参数（例如 `/plan <任务>`、`/plugin install <来源>`）不会强行弹出菜单，仍然直接输入。

GUI 使用同一份命令数据：输入 `/` 后在输入框上方弹出面板，方向键移动、Enter 选择、Esc 关闭。GUI、TUI 和远程消息渠道不会各自维护命令列表。

| 命令 | 作用 |
|---|---|
| `/help` | 显示帮助 |
| `/new` | 创建一个不继承旧上下文的空白会话 |
| `/context` | 查看上下文用量，以及整场会话/上一轮的缓存命中率和异常归因；重载会话后在跑完新一轮之前，逐轮数据来自会话文件（仍显示为 `Last turn`）；供应商未上报缓存明细时显示 `cache usage not reported`，不会误记为 0% 命中 |
| `/compact` | 立即压缩上下文（对话太长时用） |
| `/rewind [turn]` | 打开 checkpoint 菜单；也可手动指定 turn，回滚该回合及之后的文件改动，并向模型追加隐藏的精确回滚清单 |
| `/model [名字]` | 打开模型菜单；也可手动指定名字切换模型 |
| `/effort [级别]` | 打开当前模型的推理强度菜单；只显示该模型支持的级别，未知模型仅提供 `auto` |
| `/permission [manual\|auto\|full]` | 打开权限菜单；也可手动切换审批模式 |
| `/plan <任务>` | 让 AI 先出方案，你确认后再动手 |
| `/sessions [id]` | 打开历史会话菜单；也可手动指定 id 恢复会话 |
| `/export` | 导出当前对话到文件 |
| `/plugin` | 管理插件（见「插件」一节） |
| `/connect` | 连接 IM 平台（见下一节） |
| `/disconnect <平台>` | 断开某个 IM 平台 |
| `$<技能名>` | 使用技能（见「技能」一节） |

### 上下文压缩

自动压缩和 `/compact` 会在状态框实时流式展示正在生成的摘要，并显示触发方式、消息数、估算 token 用量和完成耗时。完成后以工具条目（`ฅ Compacted …`；终端过窄时标题与统计信息会分成两行）保留在消息流中；失败时保留错误信息与已收到的部分内容。压缩只裁剪发送给 LLM 的活动上下文，完整原始消息保存在 session 的 `transcript.jsonl` 中，重新加载和导出时仍然可见。每次压缩前还会在 session 的 `backups/` 目录保存当时的 `session.json`；备份失败则取消压缩。

**Jev 相关性预筛（可选）**：配置 `jev.api_key` 后（见「配置文件一览」），压缩摘要前会把将被摘要的旧工具结果批量交给 Jev 判断"对当前任务是否还有用"，低相关的直接替换为一行占位再交给 LLM 摘要——摘要更快、更聚焦，也不再为无关内容付费。Jev 调用失败或超时会自动回退到原有逻辑，判定结果有缓存（同一内容不重复计费），每次判定与应用数量都会写入应用日志（`jev: …`、`compaction: relevance pruner …`）。

### 快捷键

| 按键 | 作用 |
|---|---|
| `Enter` | 发送消息 / 确认选项 |
| `Alt+Enter` | 输入框内换行 |
| `Esc` | 停止任务 / 关闭菜单；多级菜单中返回上一级 |
| `Ctrl+C` | 输入框非空时清空内容；输入框为空时退出程序 |
| `↑` / `↓` | 翻历史输入；菜单中移动选项 |
| `Tab` / `Shift+Tab` | 循环命令或菜单候选 |
| `PgUp` / `PgDown` | 滚动聊天记录 |
| `End` | 跳到最新消息 |
| `y` / `n` | 审批弹窗中确认 / 拒绝 |

## 四、连接 IM 平台

连接后，你可以不在电脑前，通过手机上的 Telegram / 飞书 / QQ / 企业微信给 NekoCode 派任务、收结果、批审批。各平台的凭证都保存在 `~/.nekocode/connect.json`（由命令自动管理，无需手改）。

在任一已连接渠道发送 `/help` 会打开统一命令菜单；发送 `/model`、`/rewind`、`/sessions`、`/plugin` 等有限候选命令会继续打开下一级。Telegram 和飞书显示按钮，QQ、企业微信等文本交互显示编号并接受数字回复。菜单 5 分钟未操作会失效，重新发送对应命令即可。

### Telegram

1. 在 Telegram 里找 **@BotFather**，发送 `/newbot` 创建一个机器人，得到一串 token（形如 `123456:ABC-DEF...`)
2. 在 NekoCode 里执行：

   ```
   /connect telegram add <你的token>
   ```

3. 执行 `/connect telegram pair`，终端会显示一个配对链接和二维码
4. 用手机点开链接（或扫码），自动跳转到你的机器人，点击「开始」即完成绑定

之后直接在 Telegram 里给机器人发消息就能用。Telegram 的 `/` 原生命令列表会从 Runtime 自动同步；动态候选和审批请求以按钮展示。

其他常用命令：

- `/connect telegram profiles` — 查看已添加的机器人（支持多个）
- `/connect telegram use <名字>` — 切换当前使用的机器人
- `/connect telegram unpair` — 解除绑定
- `/connect telegram status` — 查看连接状态

Telegram 聊天内额外支持：`/status`（查看任务状态）、`/last`（最近一次任务结果）、`/diff`（查看代码改动）。

### 飞书

1. 登录 [飞书开放平台](https://open.feishu.cn)，创建一个企业自建应用，记录 **App ID** 和 **App Secret**
2. 在应用后台启用「机器人」能力，订阅 `im.message.receive_v1` 事件，并开启长连接模式
3. 在 NekoCode 里执行：

   ```
   /connect feishu add <App ID> <App Secret>
   ```

4. 执行 `/connect feishu pair`，得到一串配对码
5. 在飞书里**私聊你的机器人**，把配对码发给它，即完成绑定

之后私聊机器人即可派任务。命令候选和审批都会以交互卡片出现，点按钮即可处理；卡片发送失败时自动降级为编号文本。

### QQ 机器人

1. 登录 [QQ 机器人开放平台](https://q.qq.com)，创建机器人，记录 **AppID** 和 **AppSecret**
2. 在 NekoCode 里执行：

   ```
   /connect qqbot add <AppID> <AppSecret>
   ```

   保存后会自动连接，无需配对。

3. 群聊中 @机器人 或私聊机器人即可使用

注意：QQ 机器人的访问控制由平台侧管理（沙箱环境用 `/connect qqbot sandbox on` 切换），请通过平台后台控制谁能触达你的机器人。

### 企业微信智能机器人

这里使用企业微信的“智能机器人”API 长连接模式，而不是只能推送通知的群机器人 Webhook：

1. 在企业微信管理后台进入「智能机器人」，创建机器人并选择 **API 模式**
2. 连接方式选择 **使用长连接**，记录 **Bot ID** 和 **Secret**
3. 在 NekoCode 里执行：

   ```
   /connect wecom add <Bot ID> <Secret>
   ```

4. 执行 `/connect wecom pair`，得到一串配对码
5. 在企业微信中私聊机器人，或在内部群中 @机器人并发送配对码

配对成功后可以发送普通文本、语音转写文本和通用命令。运行结果、审批和提问以 Markdown 或纯文本回复；图片、文件与模板卡片暂不支持。企业微信同一个智能机器人同时只能维持一个长连接实例，不要用同一组凭证启动多个 NekoCode 进程。

### IM 聊天内通用命令

各平台在聊天里都支持：

| 命令 | 作用 |
|---|---|
| `/stop` | 停止当前任务 |
| `/approve <id>` | 批准一次操作 |
| `/always <id>` | 批准并永久允许 |
| `/reject <id>` | 拒绝操作 |
| `/answer <内容>` | 回答 AI 的提问 |
| `/dismiss` | 忽略 AI 的提问 |
| `/help` | 显示帮助 |

除命令外，发送的任何文字都会作为任务提交给当前会话。

## 五、技能（Skill)

技能是给 AI 的「专项能力包」，比如「按我们团队的规范写提交信息」「把周报整理成固定格式」。格式与 Claude Code 的技能兼容。

### 使用技能

- **自动触发**:AI 会根据技能描述自动判断何时使用
- **手动触发**：输入 `$技能名`（如 `$commit`)，带参数则直接执行，如 `$commit 修复登录问题`

NekoCode 内置以下工程工作流，平时可自动触发，也可以手动指定：

| 技能 | 用途 |
|---|---|
| `$hunt` | 先复现和验证假设，再定位或修复报错、失败测试与异常行为 |
| `$think` | 在编码前调研架构、明确取舍并提交方案等待确认 |
| `$check` | 对照目标审查实际 diff、运行验证，并复核较大或高风险改动 |
| `$skill-creator` | 创建、检查或更新自定义技能 |

这些技能只在匹配任务时加载完整工作流；主提示词只保留所有任务共同的权限、安全和证据边界。

### 安装技能

技能就是一个包含 `SKILL.md` 文件的目录，放在以下任一位置即可被发现：

- 项目级：`<项目目录>/.nekocode/skills/<技能名>/SKILL.md`（只在这个项目生效）
- 用户级：`~/.nekocode/skills/<技能名>/SKILL.md`（所有项目生效）

同名技能按「项目 → 用户 → 插件 → 内置」的顺序选择，优先级最高的完整定义生效。技能列表在启动时加载，正文按需注入对话。修改后执行 `/workspace reload` 刷新。

`SKILL.md` 的写法：

```markdown
---
name: commit
description: 按团队规范生成提交信息。当用户要求提交代码、写 commit 时使用。
---

你是一个提交信息助手。请遵循以下规范：
1. 第一行不超过 50 字……
2. ……
```

`name` 和 `description` 必填；`description` 写清楚「什么时候该用这个技能」,AI 靠它自动触发。

> 💡 偷懒技巧：直接说“帮我写一个做 XX 的技能”会自动加载 `skill-creator`，也可以显式输入 `$skill-creator`。

## 六、插件（Plugin)

插件是「打包好的能力组合」，可以一次携带技能、子代理、钩子、MCP 服务等多项内容，格式兼容 Claude Code 插件。

```bash
/plugin install <来源>      # 安装（GitHub 地址、user/repo、本地路径均可）
/plugin list                # 查看已安装
/plugin info <名字>         # 查看详情
/plugin disable <名字>      # 临时禁用
/plugin enable <名字>       # 重新启用
/plugin uninstall <名字>    # 卸载
```

示例：

```bash
/plugin install anthropics/claude-code-plugins
/plugin install ./my-local-plugin
```

远程安装前会显示插件内容预览，确认后才安装（加 `--yes` 跳过确认）。插件安装到 `~/.nekocode/plugins/`，其中的技能会自动可用。

### 自定义子 Agent

插件通过 manifest 引用 AgentMD 文件。例如本地目录 `my-reviewer/`：

```text
my-reviewer/
├── .claude-plugin/plugin.json
└── agents/reviewer.md
```

`.claude-plugin/plugin.json`：

```json
{
  "name": "my-reviewer",
  "agents": ["agents/reviewer.md"]
}
```

`agents/reviewer.md`：

```markdown
---
name: reviewer
description: 对改动做只读审查，报告有证据支持的问题
base: explore
tools: [Read, Grep, Glob]
skills: [check]
max_steps: 20
---
阅读委托指定的代码，给出问题位置、影响和证据。
只能使用当前工具；无法执行测试时如实说明，不修改文件。
```

运行 `/plugin install ./my-reviewer` 后，模型可以通过 `agent_profiles` 查询目录，再以 `task(profile="my-reviewer/reviewer", prompt="…")` 委托任务。同名 Agent 可来自不同插件；短名称只在唯一时解析，`coder`、`explore` 始终指向内置 Profile。`/plugin info my-reviewer` 及 GUI 插件详情显示可调用 ID 和加载错误。修改已安装文件后刷新扩展，或禁用后重新启用插件。

支持字段与规则：

| 字段 | 含义 |
|------|------|
| `name` | 必填；字母/数字开头，只包含字母、数字、`-`、`_` |
| `description` | 目录中展示的用途，建议填写 |
| `base` | 可选 `coder` 或 `explore`；声明后工具只能收窄到该基线的子集 |
| `tools` | 工具白名单，支持 YAML 数组或逗号分隔字符串；`Read/Write/Edit/Bash/Grep/Glob/LS/WebSearch/WebFetch` 会映射为内部工具名 |
| `skills` | 默认工作流；与本次 task 的 skills 合并、去重，在运行前解析，不授予额外权限 |
| `max_steps` | 1–50 个工具执行轮次；省略或 0 使用默认 50，达到上限返回 partial 结果 |
| Markdown 正文 | 必填的自定义指令；执行器始终保留通用子 Agent 约束和结构化完成协议 |

不写 `tools` 时，声明了 `base` 就继承基线工具，否则为无外部工具的文本任务；`tools: []` 明确禁用外部工具。无 `base` 的 Profile 可声明宿主已有工具，但不能声明 `task`、`agent_profiles` 或内部完成工具。MCP 需同时列出 `capability` 和精确目标，例如 `mcp__demo__lookup`；不支持通配授权，目标可用性在调用时检查。

Agent 文件只允许从插件目录内部读取（包含符号链接检查），最大 1 MiB。同一插件内重名、未知工具、非法字段或任何 Agent 文件加载失败都会阻止该插件本次运行时激活；不会发布其中的 Skills、Agents、Hooks 和 MCP。修复后可重新启用或刷新。模型仍继承主 Agent 配置，thinking 关闭；`model`、`permissionMode` 等未支持字段会明确报错，不会静默忽略。Skill 的 `context/agent/max_steps/context_window` 元数据本身不启动子 Agent，委托仍通过 `task`。

为兼容已有插件，manifest 省略 `agents` 或设置空数组时仍会自动发现插件中的 `agents/*.md`。需要控制加载范围时，请显式列出文件。

## 七、MCP 服务

MCP(Model Context Protocol）可以为 AI 接入外部工具和数据源（数据库、内部系统等）。在 `~/.nekocode/config.json` 中添加 `mcp_servers` 字段：

```json
{
  "mcp_servers": {
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"],
      "enabled": true
    }
  }
}
```

- 本地服务使用启动命令 `command`，可以带 `args`（参数）和 `env`（环境变量）；远程服务使用 `url`，两者不能同时指定
- `enabled` 设为 `false` 可临时停用而不删除配置
- 支持 stdio（本地进程）和 Streamable HTTP（远程服务）；远程服务支持通用 OAuth 授权
- 配置后重启 NekoCode 生效
- 模型通过统一的 `capability` 工具使用 MCP 服务（先 `list` 看可用工具，再 `call` 调用）；服务上下线不会改变模型的工具列表，缓存更稳定
- 权限规则按真实工具名书写（如 `fs__read_file`)，裸工具名即可，不需要 `(...)` 修饰符

### 远程 MCP 与 OAuth

在 GUI 的「MCP 服务」中选择「远程 URL」，填写 MCP 地址，点击「保存并授权」。需要授权时会打开本机浏览器；完成登录和授权后，NekoCode 接收回调、保存凭据并自动连接、加载工具。不需要授权的远程服务可以直接连接。

全局配置示例（项目 `.mcp.json` 中使用相同字段，外层键为 `mcpServers`）：

```json
{
  "mcp_servers": {
    "documents": {
      "url": "https://example.com/mcp",
      "enabled": true
    }
  }
}
```

TUI 中使用 `/mcp-login documents` 启动后台授权，命令立即返回，授权链接生成后通过系统消息送达；`/mcp` 查看状态和浏览器未打开时的备用链接；`/mcp-cancel documents` 取消等待，`/mcp-logout documents` 删除本机登录凭据。GUI 提供对应操作。手动编辑配置后重启生效，GUI 保存会应用配置。

OAuth 使用授权服务器发现、Authorization Code + PKCE（S256）和一次性 state 校验。支持动态客户端注册；高级设置也可填写 `oauth_client_id`（预注册客户端）、`oauth_client_secret`（机密客户端，如 GitHub 这类不支持动态注册的服务商要求在 token 端点出示 secret）或 `oauth_client_metadata_url`（已发布的 HTTPS 客户端元数据文档）。NekoCode 不替服务商创建预注册应用，也不代为托管客户端元数据文档。

回调默认监听随机的本机端口，路径为 `/oauth/callback`。若预注册客户端要求固定回调地址，设置 `oauth_callback_port`，并在服务商登记 `http://127.0.0.1:端口/oauth/callback`。浏览器与 NekoCode 必须在同一台机器；本版不提供远程部署的公网回调或 Device Code 登录。授权等待最多 5 分钟。

凭据保存在 `~/.nekocode/credentials/mcp/`，按服务名、工作目录、服务 URL 和客户端配置隔离，不写入项目配置或发送给模型。凭据文件以 `0600` 权限写入私有目录（普通文件存储，并非系统钥匙串加密）。应用重启后会恢复凭据，在请求需要时自动刷新并保存轮换后的 refresh token。服务未签发 refresh token、撤销授权或刷新凭据失效时，需要重新授权；临时网络错误可重试。后台连接不会自行打开浏览器。

远程服务必须使用 HTTPS；本机测试允许使用回环 IP 的 HTTP URL。退出登录清除本机凭据，即使服务离线也会执行；这不等同于撤销服务商账号中的应用授权。旧式 HTTP+SSE transport 和服务端 URL Elicitation 不属于本版支持范围；ACP 客户端注入的 HTTP/SSE MCP 仍按现有能力声明拒绝。

## 八、配置文件一览

### 项目工作空间

从某个目录启动 NekoCode，该目录就是本实例的项目根目录。启动时自动读取：

```text
my-project/
├── NEKOCODE.md                     # 项目规范
└── .nekocode/
    ├── .mcp.json                   # 项目 MCP
    ├── skills/
    │   └── <name>/SKILL.md         # 项目技能
    ├── plugins/                   # 可选：项目插件
    ├── permissions.json           # 自动保存的本机授权
    └── index.db                   # 自动生成的索引
```

各项独立可选，只有 `NEKOCODE.md` 也能生效；缺少 `.nekocode` 时继续使用用户级配置，不会为了加载配置而创建该目录。当前不向父目录查找项目，也不递归加载子目录的 `NEKOCODE.md`。Shell 中的 `cd` 不改变项目配置来源。

`NEKOCODE.md` 使用 UTF-8，最多 128 KiB。其内容持续作为项目指令提供给主 Agent 和受委托的子 Agent，压缩对话不会删除它；新建或恢复会话时重新读取当前版本。规范文件不授予文件访问权限。旧会话仍按保存的 `cwd` 识别项目，跨项目恢复会被拒绝，需要从原目录打开。

如果会话缺失 `cwd` 元数据，会明确提示元数据不完整；需要在该会话的 `session.json` 中恢复原项目的绝对路径后再继续，不会自动将未知来源的会话绑定到当前项目。

项目 MCP 文件使用 `mcpServers` 字段，例如：

```json
{
  "mcpServers": {
    "project-tools": {
      "command": "node",
      "args": ["tools/mcp-server.js"],
      "env": { "MODE": "development" }
    },
    "global-server-to-disable": {
      "enabled": false
    }
  }
}
```

项目 MCP 与 `~/.nekocode/config.json` 中的全局 MCP 按名称合并；同名时项目定义完整替换全局定义，不拼接参数或环境变量。项目文件中省略 `enabled` 表示启用，`enabled: false` 会屏蔽同名全局服务；全局配置的既有 `enabled` 语义不变。项目配置不会写回用户级配置文件。

MCP 子进程默认以项目根目录为工作目录；可用 `cwd` 指定绝对路径或相对项目根目录的路径。带路径的相对 `command` 基于该子进程工作目录解析，裸命令名使用 `PATH`，`args` 原样传递。当前仅支持 stdio；项目文件最大 1 MiB，空列表写作 `{"mcpServers": {}}`。

使用 `/workspace` 查看根目录、项目配置路径和读取错误，使用 `/workspace reload` 刷新；管理界面的技能刷新也会更新这些配置。刷新在任务之间执行，不监听磁盘变化。格式错误时保留该文件上一份有效配置，其他文件继续加载；删除文件后刷新会撤销其配置。MCP 连接失败显示错误状态，不会自动改用同名全局服务。

项目 MCP 覆盖连接提供的同名 MCP 时保留原定义，删除覆盖项并刷新后会恢复原服务；连接已撤销的定义不会恢复。管理界面将被其他来源覆盖的 MCP 标记为 `shadowed`，不会将实际运行服务的状态或工具数量归给被覆盖项。

每次项目重载会重试处于错误状态的 MCP，包括显式刷新、新建和恢复会话。正常运行或正在启动的配置 MCP 会保留；单纯打开管理界面或读取状态不会触发重试，也没有后台循环重试。

`NEKOCODE.md` 是单个文件，可按需纳入版本管理。`.nekocode/` 下是运行数据（技能、插件、MCP 配置、权限和索引），NekoCode 不管理项目的忽略规则，建议在项目 `.gitignore` 中整体忽略 `.nekocode/`。MCP 的 `env` 可能包含密钥，需要共享配置结构时，可提交脱敏的 `.mcp.example.json`，再由使用者复制为 `.mcp.json` 并填入本机配置。示例文件不会自动加载。会话、导出和日志继续保存在用户目录。当前仍是单实例绑定单项目，不提供运行中切换项目或同进程多项目执行。

### 文件位置

| 文件 | 用途 | 需要手改吗 |
|---|---|---|
| `~/.nekocode/config.json` | 主配置：模型、MCP、权限、工作区 | ✅ 需要（至少配一次模型） |
| `~/.nekocode/connect.json` | IM 平台凭证和配对状态 | ❌ 由 `/connect` 命令自动管理 |
| `<项目>/NEKOCODE.md` | 项目规范，自动加载 | ✅ 可选 |
| `<项目>/.nekocode/.mcp.json` | 项目 MCP，覆盖同名全局定义 | ✅ 可选 |
| `<项目>/.nekocode/skills/` | 项目技能目录 | ✅ 可选 |
| `<项目>/.nekocode/permissions.json` | 「始终允许」记录的授权规则 | ❌ 审批时自动写入 |
| `~/.nekocode/memory.md` | 长期记忆，自由书写的 Markdown,AI 每轮都会参考 | ✅ 可选 |
| `~/.nekocode/sessions/` | 会话存档 | ❌ 自动管理 |
| `~/.nekocode/exports/` | `/export` 导出的对话 | ❌ 自动管理 |

### config.json 综合示例

```json
{
  "active": "deepseek",
  "flash_model": "deepseek",
  "auto_compact_percent": 80,
  "models": [
    {
      "name": "deepseek",
      "provider": "deepseek",
      "api_key": "sk-xxx",
      "model": "deepseek-v4-flash",
      "base_url": "https://api.deepseek.com/v1",
      "protocol": "openai",
      "reasoning_effort": "medium"
    },
    {
      "name": "claude",
      "provider": "anthropic",
      "api_key": "sk-ant-xxx",
      "model": "claude-sonnet-4-5",
      "base_url": "https://api.anthropic.com",
      "protocol": "anthropic",
      "reasoning_effort": "high"
    }
  ],
  "image_gen_models": [
    {
      "name": "jimeng",
      "provider": "jimeng",
      "api_key": "access-key",
      "secret_key": "secret-key",
      "model": "jimeng_t2i_v31",
      "base_url": "https://visual.volcengineapi.com"
    }
  ],
  "mcp_servers": {
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"],
      "enabled": true
    }
  },
  "permissions": {
    "allow": ["Bash(npm run *)", "Read"],
    "ask": ["Bash(git push *)"],
    "deny": ["Bash(rm -rf *)"],
    "sandbox": {
      "Bash(go test *)": {
        "sandbox_mode": "workspace-write",
        "network": false,
        "writable_roots": ["/tmp"]
      }
    }
  },
  "workspaces": [
    {"path": "/home/me/other-project", "access": "read-only"}
  ],
  "jev": {
    "api_key": "apikey-xxx"
  }
}
```

主要字段说明：

- `active` / `models`：模型配置（见「首次配置」）
- `flash_model`：可选的轻量模型配置名，用于压缩和子 Agent 等辅助请求；省略时跟随 `active`
- `image_gen_models`：可选的图片生成模型列表；只有配置后才会注册 `image_gen` 工具
- `reasoning_effort`：可选的模型推理强度，留空或设为 `auto` 时使用模型默认值。可选级别由模型能力表决定，`/effort` 和配置界面只展示当前模型支持的值；未知模型严格退化为 `auto`，`none`（界面显示为 Off）也只在模型明确支持关闭推理时出现。OpenAI 与 Anthropic 协议适配器只翻译已解析的生效值，不根据协议猜测模型能力
- `context_window`：上下文窗口大小。**这是模型的属性，通常不用填**——NekoCode 内置了常见模型的对照表，会根据模型名自动确定（如 deepseek 1M、Claude 200K~1M、Gemini 1M)。需要精确控制时（比如自部署模型），在 `models[]` 里给对应模型填 `context_window`：单模型覆盖 > 内置表 > 默认 128K。GUI 概览只读显示当前模型的有效窗口；模型卡片中的“上下文窗口覆盖”留空即保持自动解析，不会把默认值固化进配置
- `auto_compact_percent`：自动摘要压缩的上下文占用门限，范围 1～99，默认 80。达到门限后执行一次全量摘要替换；压缩后若仍达到模型窗口上限，才返回上下文已满错误
- `permissions`：权限规则（见下一节）；`allow`、`ask`、`deny` 分别表示允许、询问和拒绝，`sandbox` 可为匹配的 Shell 命令指定 `read-only`、`workspace-write` 或 `host` 执行环境
- `workspaces`：允许 AI 访问的项目外目录，`access` 为 `read-only` 或 `read-write`
- `jev`：可选的 Jev 决策引擎（[TypeSafe AI](https://docs.typesafe.ai) 的 "System One" 模型，不生成文本、只返回带概率的结构化判断）。**只需填 `api_key`**（也可改用 `TYPESAFE_API_KEY` 环境变量），其余字段全部有默认值。配置后启用两项能力：
  - **压缩相关性预筛**：见「上下文压缩」一节，降低摘要成本并提升摘要聚焦度
  - **`/permission auto` 模式**：见「权限与安全」一节，shell 命令经 Jev 优先判定安全性，安全则免审批执行
  - 可选字段：`model`（默认 `jev-latest`；生产环境建议锁定 `jev-1.x` 版本以稳定阈值）、`base_url`（默认官方端点）、`keep_threshold`（0～1，压缩预筛的保留阈值，默认 0.2，数值越高保留越多；auto 权限门禁使用独立的固定安全阈值，不受此项影响）、`enabled`（总开关，`false` 显式关闭 Jev——即使 `api_key` 或环境变量存在也不启用；省略或 `true` 为开启）
  - 未配置或 Jev 服务不可用时，所有功能自动回退原有逻辑，无任何行为差异；判定明细可在应用日志中用 `jev:` 前缀检索

## 九、权限与安全

NekoCode 对有风险的操作默认会征求你的同意，规则按 **拒绝 > 询问 > 允许** 的优先级生效：

- **内置保护**：危险命令（如 `sudo`、`dd`）直接拒绝；删除文件、推送代码等操作会先询问；未读过的文件不允许修改
- **声明规则**：在 `config.json` 的 `permissions` 中提前声明，格式为 `工具(范围)`:

  ```json
  "permissions": {
    "allow": ["Bash(npm run *)"],
    "deny":  ["Bash(rm *)"]
  }
  ```

- **MCP 工具规则**：模型通过 `capability` 代理调用 MCP 工具，但规则按 canonical **真实工具名**（`mcp__服务名__工具名`，如 `mcp__fs__read_file`）匹配和书写；审批弹窗、Hook、审计与 Ledger 也使用该名称，裸工具名即可（如 `"allow": ["mcp__fs__read_file"]`)，不需要 `(...)` 修饰符
- **审批时记住**：弹窗里选「始终允许」，同类操作以后自动放行（记录在项目 `.nekocode/permissions.json` 里，删除该文件可清空）
- **auto 模式（可选，需配置 Jev）**：`/permission auto` 开启后，未匹配用户 deny、ask、allow 的 shell 命令会交给 Jev 判断危险性——判定安全（置信度足够高）的命令直接执行，危险**或不确定**的照常弹出授权框。安全设计：硬拒绝规则和用户声明的规则优先于 Jev；内置询问规则是 Jev 判定危险或不可用时的回退；Jev 超时、评分缺失或越界，以及命令超过 960 个字符时，都回退为询问；Jev 放行后仍会经过间接执行、管道注入等结构化检测。日志仅记录判定分数、缓存统计和输入摘要，不记录可能含密钥的命令或 URL 原文。auto 与 full 互斥：切到 full 会关闭 auto，反之亦然
- **动态 Shell**：命令替换、进程替换、动态命令名、`eval`、`source`、`shell -c` 和 shell heredoc 会明确标注在审批卡上。宽泛的 `Bash(*)` 不会绕过这次审批；选择「始终允许」只会记住完整命令字面量，不会自动放宽成 glob

## 十、常见问题

**启动后聊天报错？**
检查 `~/.nekocode/config.json` 里的 `api_key` 是否填写、`base_url` 是否正确、`active` 是否和 `models` 里的 `name` 对上。

**改完配置没生效？**
重启 `nekocode-tui`。

**IM 平台连不上？**
先用 `/connect <平台> status` 查看状态和错误提示；Telegram 确认 token 没复制错，飞书确认已开启长连接模式并订阅了消息事件，企业微信确认创建的是 API 模式智能机器人并选择了长连接。

**配对码过期了？**
配对码有效期 5 分钟，重新执行 `/connect <平台> pair` 获取新码。

**想清空「始终允许」的记录？**
删除项目目录下的 `.nekocode/permissions.json` 即可。

**Jev 判定不准或想临时关闭？**
在 `config.json` 的 `jev` 段加 `"enabled": false` 即可一键关闭（保留 api_key，随开随用），改完通过配置接口保存会立即生效，否则重启生效；也可以直接删除 `jev` 段并取消 `TYPESAFE_API_KEY` 环境变量。关闭后当前 auto 模式会自动切回 manual，且菜单不再显示 auto。判定明细在应用日志中以 `jev:` 前缀检索，便于确认是判定质量问题还是阈值设置问题（可在 `keep_threshold` 调整保留倾向）。
