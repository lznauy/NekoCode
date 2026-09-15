# Subagent demo

这个插件提供 `subagent-demo/reviewer`：只读文件和搜索，默认组合内置 `check` Skill，最多执行 6 个工具轮次。

在仓库根目录启动 NekoCode 后，输入：

```text
/plugin install ./examples/subagent-demo
/plugin info subagent-demo
```

然后发送：

```text
先用 agent_profiles 查询 subagent-demo/reviewer，再委托它只读审查
bot/extension/agentprofile/load.go。请传递该文件的绝对路径，
报告有证据的问题或明确说明未发现问题，不要修改文件。
```

模型通过 `task` 的 `profile` 选择 `subagent-demo/reviewer`，`prompt` 传入具体范围。可以不传 `skills`，运行时会加载 Profile 默认的 `check`。可用工具为 `read`、`grep`、`glob`，`shell`、`write`、`edit` 不可用。

## 自动接入验证

从仓库根目录运行：

```sh
go test ./bot/core -run '^TestSubagentDemoEndToEnd$' -count=1 -v
```

测试使用本目录的真实插件文件、标准 Bot 和本地 HTTP/SSE 模拟模型，在临时工作区中验证：

- 冷启动发现 manifest，并返回可调用 Profile。
- 主 Agent 查询 `agent_profiles` 后调用 `task`。
- 子 Agent 收到自定义提示、默认 `check` Skill 和过滤后的工具。
- 实际读取临时文件；即使模型请求 `shell`，执行器也会拒绝。
- 子 Agent 提交结构化结果，主 Agent 收到交接，开始/工具/结束事件关联一致。

不需要 API key，不访问外部模型，也不安装到真实用户的插件目录。此测试证明接入和执行边界；真实模型是否正确选择并完成审查，需要按上面的交互步骤另行验证。

使用完手动安装的示例后，可运行 `/plugin uninstall subagent-demo`。
