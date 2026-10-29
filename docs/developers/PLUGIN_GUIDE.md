# 自定义 Plugin 指南

> **主要读者：** 用户侧扩展作者。组合与分发扩展，不涉及官方版本发布。

Plugin 把 Skill、子 Agent、Hook 和 MCP 配置组织成一个可安装目录。本文创建 `review-kit`：提供一份审查工作流、一个只读审查员，以及运行开始时的提示 Hook。

这个示例不依赖外部命令或 MCP。安装和配置加载可以离线验证，实际模型委托仍需要配置模型服务。

## 一、准备完整目录

```text
review-kit/
├── .claude-plugin/
│   └── plugin.json
├── skills/
│   └── change-review/
│       └── SKILL.md
├── agents/
│   └── reviewer.md
└── hooks.json
```

先只创建这四个文件。插件不是可执行程序，不需要额外的 `main` 入口，也不需要为了生效添加 `install.sh`。

## 二、填写 manifest

`.claude-plugin/plugin.json`：

```json
{
  "name": "review-kit",
  "version": "0.1.0",
  "description": "只读审查工作流与子 Agent",
  "skills": ["skills"],
  "agents": ["agents/reviewer.md"],
  "hooks": {
    "source": "hooks.json"
  }
}
```

路径都相对于 `review-kit/`，而不是 `.claude-plugin/`。`skills` 指向目录，`agents` 指向 Markdown 文件，`hooks.source` 指向 Hook 配置。

`name` 是安装和管理标识，建议只用字母、数字、连字符。更新同一插件时保持名称一致；改名称会变成另一个插件。`version` 用来显示版本，插件管理器不会仅因版本号增加就自动下载更新。

其他支持字段包括 `author`、`repository`、`license`、`keywords` 和 `mcpServers`，字段定义见 [manifest 类型](../../bot/extension/plugin/manifest.go)。新插件只填写当前实现支持的字段，不要假设其他工具的全部插件字段都能在这里工作。

## 三、加入 Skill

`skills/change-review/SKILL.md`：

```markdown
---
name: change-review
description: 用户要求审查明确指定的代码文件、检查正确性和维护性时使用。
---
先确认审查范围，读取目标文件和直接相关的调用方。

每项发现说明文件位置、触发条件、影响和证据，不把猜测当作确定问题。
只执行当前工具允许的验证；没有运行测试时明确说明。
除非用户要求修复，只报告问题，不修改文件。
未发现问题时如实报告，不为了凑数量提出无依据的问题。
```

这份工作流既可在主对话里用 `$change-review` 调用，也可作为子 Agent 的默认技能。项目或用户目录中的同名技能可能覆盖它；插件技能不会自动变成 `review-kit/change-review` 这样的命名空间。详见 [Skill 指南](SKILL_GUIDE.md)。

## 四、加入 Agent

`agents/reviewer.md`：

```markdown
---
name: reviewer
description: 按审查工作流阅读指定代码并报告有依据的问题
base: explore
tools: [Read, Grep, Glob, LS]
skills: [change-review]
max_steps: 20
---
只审查委托中指定的文件及直接相关代码，不修改文件。
遵循已加载的审查技能，报告具体位置、依据及尚未完成的验证。
无法执行的验证必须说明，不能声称已经运行测试。
```

安装后可调用 ID 为 `review-kit/reviewer`。Profile 的工具范围是读取与搜索，不包含 Shell；Skill 不会增加这些权限。更多字段见 [Agent 指南](AGENT_GUIDE.md)。

## 五、加入 Hook

`hooks.json`：

```json
{
  "version": 1,
  "RunStart": [
    {
      "id": "review-reminder",
      "mode": "observer",
      "hooks": [
        {
          "type": "js",
          "code": "({hints:[{type:'workflow',severity:'info',content:'执行审查任务时，请区分已确认的问题与尚未验证的猜测。'}]})",
          "timeout": 1000
        }
      ]
    }
  ]
}
```

这个 Hook 在父运行开始时提供提示，便于验证插件 Hook 已激活。它不会强制所有任务变成代码审查，也不对每个子 Agent 工具调用执行检查。

JS 是内联代码，因此不需要额外脚本文件。复杂逻辑可以移到插件内文件，以 `path` 引用；完整示例、状态和日志排查见 [Hook 指南](HOOK_GUIDE.md)。

## 六、安装并逐项验证

从能访问 `./review-kit` 的项目启动 NekoCode，在输入框中执行：

```text
/plugin install ./review-kit
/plugin list
/plugin info review-kit
/plugin check review-kit
```

本地安装会复制目录到 `~/.nekocode/plugins/`。管理信息中的 `Directory` 是实际安装位置；源目录后续修改不会自动同步。

按顺序验证三项能力，便于定位问题：

1. 输入 `$change-review 审查 bot/policy/contract/result.go，只报告问题。`，确认技能能够加载。模型正文中应遵循审查范围和验证要求。
2. 请主 Agent 使用 `agent_profiles` 查找 `review-kit/reviewer`，再委托它读取上述文件。调用 ID 和工具上限应与配置一致。
3. 在应用日志中检索 `review-reminder`，确认有 `run_start` 的 Hook 执行记录。日志默认在 `~/.nekocode/logs/`。

前两项需要目标文件存在且模型可用；在其他项目测试时，替换为该项目真实文件。不要把“安装成功”当成模型行为测试已通过。

## 七、修改、停用与卸载

| 操作 | 在 NekoCode 中执行 | 作用 |
| --- | --- | --- |
| 查看实际安装目录与错误 | `/plugin info review-kit` | 检查管理状态及 Agent、Hook 加载错误 |
| 停用 | `/plugin disable review-kit` | 保留文件，停用运行时扩展 |
| 启用 | `/plugin enable review-kit` | 加载已安装文件 |
| 卸载 | `/plugin uninstall review-kit` | 删除已安装插件 |

修改源目录后重新执行 `/plugin install ./review-kit`。修改已安装副本后，在任务结束时禁用再启用，或重启应用。JS 在加载时编译，不会监听磁盘变化；重复 enable 一个正常启用的插件不能代替重载。

插件停用后，其 Agent 和 Hook 不再用于后续调用。已写入对话的 Skill 正文和已经发生的外部副作用不会从历史中撤销；验证停用效果时使用新的运行，必要时新建会话。

## 八、分享给其他人

把整个 `review-kit/` 作为仓库根目录提交，使 `.claude-plugin/plugin.json` 位于仓库根目录下。其他人可使用 GitHub 地址、`owner/repository` 或本地路径安装。

远程安装会预览插件内容并请求确认；`--yes` 可跳过预览确认。远程分发前，检查提交中是否误带密钥、个人路径或运行数据。`install.sh` 不会自动执行，依赖安装步骤需要另行说明。

新版本需要使用者重新安装，不会自动更新。插件声明了权限相关能力，也仍须遵守宿主的审批、沙箱和工具配置。

## 九、扩展与加载失败

| 现象 | 检查方向 |
| --- | --- |
| `no .claude-plugin/plugin.json` | 安装路径是否指向插件根目录，是否包含隐藏目录 |
| 目录数量看起来正确却无法调用 | 查看 Agent、Hook 加载错误；文件发现不等于运行时激活 |
| 一个 Agent 出错导致 Skill 也不可用 | Agent 与 Hook 校验失败会阻止本次整体激活，先修复错误 |
| Skill 单独缺失 | 技能 frontmatter 可能解析失败而被跳过；也可能被同名技能覆盖 |
| 修改后未变化 | 源目录与安装副本是否混淆，是否完成重新安装或重载 |
| MCP 未就绪 | MCP 后台连接独立诊断，不能只看插件 enabled 状态 |

新插件建议显式声明扩展路径。省略 `skills`、`agents`、`hooks` 或 `mcpServers` 时，当前实现会按各自规则自动发现相应文件；`skills: []`、`agents: []` 也不会禁止对应的自动发现。不要把不想加载的示例放在同一个插件的发现目录内。

添加 MCP 时，可在 manifest 的 `mcpServers` 中声明服务，具体配置见 [用户指南](../users/USER_GUIDE.md#七mcp-服务)。插件 MCP 的 `${PLUGIN_ROOT}`、`${CLAUDE_PLUGIN_ROOT}` 可用于相对安装位置的路径展开。MCP 可能在后台连接，服务可用性需要单独确认。

插件可以组合这些扩展，但它们各自保留原有边界：Skill 是流程，Agent 是执行配置，Hook 是事件回调，MCP 是工具服务。

## 十、先检查兼容性

在输入框执行 `/plugin check review-kit` 检查指定插件，或 `/plugin check` 检查所有已安装插件。禁用插件也能检查；检查不会重新加载、启用或执行插件，不运行 Hook、安装脚本或 MCP 命令，也不会连接远端服务。现有已启用插件的运行状态保持不变。

报告先显示每个插件的错误/警告汇总，再按错误、警告、通过的顺序列出文件诊断。交互菜单中可选择“All plugins”检查全部插件，也可选择单个插件。

三种结果的含义：

| 标记 | 含义 |
| --- | --- |
| `ok` | 该部分通过静态解析，运行时仍需要实际验证 |
| `warning` | 有字段会被忽略、旧 commands 需要迁移，或依赖宿主配置 |
| `error` | 文件缺失/越界、配置不兼容，或扫描不完整，需要先处理 |

检查复用实际 Skill、Agent 和 Hook 解析器；MCP URL 使用与连接阶段相同的 HTTPS/回环 HTTP 校验，并拒绝同时配置 command 和 URL。报告会提示未知 manifest 字段、不支持的 Skill 执行字段、Agent 未知字段、重复名称及失效的声明路径。旧 `commands` 不会注册，工作流应迁移到 Skill；`install.sh` 仍需自行审查后运行。

这是静态检查：模型名称、外部技能、MCP 命令依赖、认证和服务连接仍需按报告提示验证。跨插件或项目/用户技能覆盖也应以 `/skills` 的当前可见结果为准。检查不打印 MCP URL、环境变量、请求头或密钥值。

单文件读取上限 1 MiB，组件发现最多 8192 个条目，技能发现最多 4096 个条目，最多展示 256 条诊断。超限或目录无法读取会明确报检查不完整，不能当成检查通过。
