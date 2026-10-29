# 自定义 Agent 指南

> **主要读者：** 用户侧扩展作者。通过插件定义和使用子 Agent，不需要修改 NekoCode 源码。

本文介绍如何给 NekoCode 增加一个可委托的子 Agent。示例创建只读审查员 `review-agents/reviewer`，通过插件安装，再由主 Agent 调用。

这里的自定义 Agent 是子任务执行配置（Profile），不是新的模型服务。它定义指令、工具范围、默认技能、步数和可选工作模型；不指定模型时跟随主 Agent。应用底座开发见 [Runtime 应用开发指南](RUNTIME_APP_GUIDE.md)。

## 一、先选 Agent 还是 Skill

| 需要 | 选择 |
| --- | --- |
| 在当前对话中复用审查步骤 | [Skill](SKILL_GUIDE.md) |
| 独立委托任务，并限制执行者的工具范围 | 自定义 Agent |
| 把 Agent、Skill、Hook 一起安装和分享 | [Plugin](PLUGIN_GUIDE.md) |
| 在确定时机执行代码检查 | [Hook](HOOK_GUIDE.md) |

Agent 的 `tools` 是执行范围上限。Skill 是工作流说明，不能扩大这个上限，也不会绕过审批、沙箱或写前读要求。

## 二、创建只读审查员

### 1. 准备插件目录

```text
review-agents/
├── .claude-plugin/
│   └── plugin.json
└── agents/
    └── reviewer.md
```

`.claude-plugin/plugin.json`：

```json
{
  "name": "review-agents",
  "version": "0.1.0",
  "description": "只读审查子 Agent",
  "agents": ["agents/reviewer.md"]
}
```

所有路径相对于插件根目录。当前自定义 Profile 由插件加载，不是把文件随意放进项目 `agents/` 就能生效。

### 2. 定义能力和指令

`agents/reviewer.md`：

```markdown
---
name: reviewer
description: 阅读指定文件，报告有证据的正确性和维护性问题
base: explore
tools: [Read, Grep, Glob, LS]
max_steps: 20
---
只审查委托中明确指定的文件和问题，不修改文件。

1. 先阅读相关文件，确认上下文和调用关系。
2. 对每个问题说明位置、触发条件和影响；没有证据时不要当作确定结论。
3. 不能执行测试时，明确说明哪些结论尚未验证。
4. 返回问题列表及必要的后续验证建议；未发现问题时如实报告。
```

这里选择 `base: explore`，再把工具收窄到读取和搜索。没有 Shell，因此审查员不能运行 `git diff`、测试或外部命令。委托时应提供文件范围和必要的差异材料。

### 3. 安装与确认

在 NekoCode 输入框执行，以下不是终端命令：

```text
/plugin install ./review-agents
/plugin info review-agents
```

详情应显示 `review-agents/reviewer`。如果显示 Agent 加载错误，先修复再启用，不能只根据“文件已安装”判断 Profile 已可用。

### 4. 委托任务

可以向主 Agent 提出：

```text
先查看 agent_profiles，确认 review-agents/reviewer 可用。
再委托它只读审查 bot/policy/contract/result.go，重点检查结果合并逻辑。
不要修改文件，也不要宣称执行了测试。
```

模型先通过 `agent_profiles` 查看可用 ID，再调用 `task`。对应的工具参数示意如下，不是可直接输入的斜杠命令：

```json
{
  "profile": "review-agents/reviewer",
  "prompt": "只读审查 bot/policy/contract/result.go 的结果合并逻辑，报告具体位置、触发条件和影响。"
}
```

自定义 ID 为 `插件名/Agent 名`。短名称只在唯一时解析；有多个 `reviewer` 时使用完整 ID。`coder`、`explore` 始终指向内置配置。

TUI 中用 `/tasks` 或 `Ctrl+G` 查看当前父会话的子任务。达到步数上限会返回部分完成结果，不表示任务失败或全部完成；实际模型执行也取决于模型与工具配置。

## 三、字段怎么选

| 字段 | 当前行为 |
| --- | --- |
| `name` | 必填；字母或数字开头，其余只允许字母、数字、`-`、`_` |
| `description` | 建议填写；显示在可用目录中 |
| `base` | 可选 `explore` 或 `coder`；声明后工具只能是基线的子集 |
| `tools` | 工具白名单；支持 YAML 数组或逗号分隔字符串 |
| `skills` | 默认技能名称数组，与 task 指定技能合并、去重，运行前解析 |
| `model` | 可选，引用 `config.json` 中 完整模型引用（`服务名/模型名或别名`）；省略或空字符串跟随主对话模型，未知名称在运行前报错 |
| `isolation` | 可选 `shared`（默认）或 `worktree`；后者要求独立 Git 工作副本，不能由 task 降回 shared |
| `max_steps` | 1–200；省略或 0 使用默认 150，按工具执行轮次限制 |
| 正文 | 必填；作为自定义指令，运行时仍保留通用约束和完成协议 |

工具名推荐使用内部小写名称。兼容别名包括 `Read → read`、`Write → write`、`Edit → edit`、`Bash → shell`、`Grep → grep`、`Glob → glob`、`LS → list`、`WebSearch → web_search`、`WebFetch → web_fetch`。

三个配置的区别要分清：

- 写了 `base`，没写 `tools`：继承该基线工具。
- 写了 `tools: []`：禁用全部外部工具，即使声明了 `base`。
- 两者都没写：没有外部工具，只能根据给定内容完成文本任务。

`coder` 包含读写文件和 Shell 等工具，`explore` 不包含写入或 Shell。完整基线见 [Profile 定义](../../bot/extension/agentprofile/profile.go)。未知工具、超出基线的工具和未知 YAML 字段都会报错；`permissionMode` 尚不支持。

### 为 Agent 指定模型

在 frontmatter 中加入 `model: relay/fast-review`，其中 `relay/fast-review` 必须是已经配置的 完整模型引用（`服务名/模型名或别名`），不是供应商模型 ID，也不是自动识别的 `fast`/`small` 别名。省略或空字符串表示使用当前主对话模型。`agent_profiles` 会展示显式模型配置。

每次任务创建独立客户端，协议、端点、推理设置及上下文窗口跟随所选模型，输出限制沿用对应协议客户端的默认限制。Profile 工具白名单保持生效；选模型不会增加工具权限。子 Agent 仍使用文本任务输入，原生图片处理不随此字段开启。

`compaction_model` 继续独立用于压缩，不再决定子 Agent 的工作模型；压缩仍关闭推理。旧版依赖 `compaction_model` 执行子任务的配置，可在相应 Profile 中显式设置同名 `model`。

恢复任务时，重新读取当前 Profile 的模型配置；修改 Profile 或模型配置后，恢复执行可使用新模型。已保存的历史内容会保留，上下文窗口和推理设置使用当前模型的值。模型配置通过现有配置刷新或重启生效，运行中不切换；缺失模型不会静默回退。

### 独立工作副本

给 Profile 加 `isolation: worktree`，或委托时使用 `task(profile="coder", isolation="worktree", prompt="…")`。未设置时沿用共享工作区。`agent_profiles` 展示 Profile 的配置；`task_list` 的已保存任务展示实际 isolation。

新副本从当前 Git HEAD 创建，不带主目录未提交、暂存或未跟踪的内容。项目必须已有 Git 提交；父级工作目录在 HEAD 中也必须存在。副本位于 `~/.nekocode/worktrees/<父会话>/<任务 ID>/tree`，保留父级相对仓库根的工作目录位置。每次执行独立创建 Shell、进程管理、LSP 和索引实例，文件缓存及内容快照独立，不能把子任务写入父会话的 rewind 记录。审批模式仍实时继承父级，`$WORKSPACE` 权限锚点指向子目录。

首版支持内置工具。会捕获父级运行状态的扩展工具（包括 capability/MCP）不能直接复用，Profile 含这类工具时明确报错；可通过预加载的 `skills` 使用工作流说明。Git 管理操作禁用 hooks、文件过滤器、外部 diff、自动取回对象和远程协议；Git LFS 等过滤器生成的文件不会自动物化，子模块也不自动初始化。

恢复任务默认沿用保存的 isolation，复用原目录和改动，不能切换模式。失败、取消和异常退出保留副本；副本被清理后该任务不能再恢复，需要新建任务。创建中断留下的记录也能用显式 discard 清理。

结果通过以下命令管理（等当前任务结束后执行）：

- `/worktree list`：列出当前父会话保留的副本，包括创建失败的记录。
- `/worktree diff <任务 ID>`：相对创建时的提交查看全部改动，包括新增的非忽略文件、已跟踪的忽略文件及子任务内的提交；暂存或提交后的强制添加文件也会保留，不改变子任务暂存区。
- `/worktree apply <任务 ID>`：预检查后应用到主工作副本；冲突报错并保留结果，不创建提交，也不自动解决冲突。多个任务和会话的应用由仓库共享锁串行执行。非空且预检通过的应用会捕获所有受影响路径并创建 `/rewind` 检查点；空差异和冲突不创建锚点。
- `/worktree remove <任务 ID>`：只清理没有改动或额外文件的副本。
- `/worktree remove <任务 ID> --discard`：明确丢弃副本中的全部内容，也适用于创建中断的半成品。任务历史与锁文件保留。

工作中的任务持有操作系统锁，管理命令不能操作其副本；进程退出会自动释放锁。应用后需在主目录审查和验证，再显式清理。Apply 与 patch 使用同一临时 index 的 raw `-z --no-renames` 计划捕获常规文件内容和权限；rename 捕获新旧两路径，symlink、gitlink 等检查点不支持的类型会拒绝应用。`/rewind` 不保证进程崩溃、外部并发写入或目录元数据恢复。单次差异输出限制为 64 MiB，超限报错并保留现场。不要删除正在使用的锁文件，也不要同时用外部编辑器或 Git 命令改写正在应用的主目录文件。

worktree 提供文件副本隔离；文件访问和命令执行仍由现有沙箱与权限规则约束，显式授予的宿主权限继续有效。

## 四、组合 Skill 与 MCP

安装好 [Skill 指南](SKILL_GUIDE.md) 中的 `change-review` 后，可在 Agent 的 frontmatter 中加入：

```yaml
skills: [change-review]
```

这是增加一行，不是用它替换完整文件。技能会在委托运行前解析；安装 Profile 时不一定能发现技能缺失，调用时仍可能报 `unknown delegated skill`。也可以在 `task` 参数中使用 `"skills": ["change-review"]`。

默认技能由宿主预先提供给子 Agent，不要求给它额外的 `skill` 工具。技能中写了“运行测试”也不能让只读 Agent 获得 Shell。

要使用 MCP，须使用宿主已注册的 `capability` 工具，并同时列出精确目标，如 `mcp__documents__lookup`。`base: explore` 不包含这些能力，不能在该基线上直接增加。自定义 MCP Profile 可不声明 base，再显式填写需要的能力；目标在调用时检查可用性，不支持通配授权。

## 五、更新与排查

安装会复制源目录到 `~/.nekocode/plugins/`。改了源目录应重新安装；改了安装副本应在任务之间禁用再启用，或重启应用：

```text
/plugin disable review-agents
/plugin enable review-agents
/plugin info review-agents
```

| 现象 | 检查方向 |
| --- | --- |
| 目录里找不到 Profile | 插件是否启用，manifest 路径和加载错误是否正确 |
| `ambiguous agent profile` | 使用完整 `插件名/Agent 名` |
| `tool exceeds base profile` | 工具超出 base 上限，收窄白名单或重新选择基线 |
| `unknown agent tool` | 工具名是否正确、宿主是否实际注册 |
| 明明安装了 Skill 仍找不到 | 确认当前项目的技能目录、名称和插件激活状态 |
| 无法运行测试 | 查看工具上限是否包含 `shell`；示例只读配置本来就没有 |

Agent 文件必须位于插件目录内，为不超过 1 MiB 的普通文件。一个插件内 Agent 重名或任何 Agent 文件加载失败，会阻止该插件本次整体激活，包括其中的 Skill、Hook 和 MCP。

manifest 省略 `agents` 或填空数组时，仍会自动发现插件内 `agents/*.md`；空数组不表示禁止自动发现。需要可控加载范围时，显式列出目标文件。

父 Agent 的插件工具 Hook 不会直接复用到子 Agent；每个执行者保留自己的工具授权边界。更多边界见 [Hook 指南](HOOK_GUIDE.md)；子 Agent 后续能力见 [规划目录](../plan/README.md)。
