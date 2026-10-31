# 行为评测

独立 Go module，仅依赖 Go 1.24+ 标准库及 Git。不导入 NekoCode 包、不读取应用配置或凭据，也不接入主项目的测试、构建或 CI。可将整个目录复制到其他位置独立运行。

## 本地验证

```bash
cd evals
go test ./... -v -count=1
```

默认只验证夹具、评分和版本隔离；真实模型评测会跳过。离线回放通过不代表模型行为通过。

测试夹具和临时 Git 仓库都位于系统临时目录，评测结果输出到 stdout，不修改待测仓库。

## 真实模型对照

在环境中显式配置以下变量，不复用应用凭据：

- `NEKOCODE_PROMPT_EVAL_MODEL`：模型名称。
- `NEKOCODE_PROMPT_EVAL_BASE_URL`：兼容 OpenAI Chat Completions 协议的接口根地址，例如 `https://example.com/v1`。
- `NEKOCODE_PROMPT_EVAL_API_KEY`：接口密钥。
- `NEKOCODE_PROMPT_EVAL_BASE_REF`：基线 Git revision，默认 `HEAD`；须与当前内容不同。
- `NEKOCODE_PROMPT_EVAL_REPEATS`：每个场景、每个版本运行次数，默认 `1`，允许 `1..5`。
- `NEKOCODE_PROMPT_EVAL_REPO`：待测仓库根目录，默认当前目录的上一级；复制本模块到其他位置后应显式设置。

```bash
go test -run TestPromptBehaviorEval -v -count=1 -timeout 30m
```

基线通过只读 Git 命令读取指定 revision 的 Markdown，候选仅读取工作区的 Markdown；模块自行提取 hunt/check 正文，不调用项目的提示词构建器或技能加载器。两版使用相同模型、场景及工具，并交替运行顺序。每轮最多 16 次模型请求，每个版本有 5 个场景；增加重复次数会增加调用成本。

日志中的 `BEHAVIOR_COMPARISON` JSON 包含基线 commit、指令哈希、工具轨迹、失败分类及待人工审阅的最终回答，随后输出两版的通过数与失败项计数。基线的行为失败作为对照数据；当前版本的行为失败或任一版本的接口错误使测试失败。

这是受限算术夹具中的提示词决策评测，不是完整 Agent 端到端测试。不自动评价根因解释是否正确、询问是否多余，也不能用少量样本证明统计优势。

## 审查误报与漏报

`go test -run TestReviewBehaviorEval -v -count=1 -timeout 30m` 使用同一组显式环境变量，对照基线与工作区的系统提示词、check 技能。四个场景分为两对：清理逻辑委托给包装函数 / 实际绕过清理函数，以及 TUI 用“权限”展示 capabilities / 实际遗漏能力范围。模型看到相同的核实请求和独立夹具，必须自行读取完整文件；修复版本不会作为正确答案泄露给模型。

夹具仅提供内存中的 read、字面量 search 和 skill 工具，不执行模型生成的代码或命令，不修改待测项目。每场景、每版本最多 16 次请求，按 REPEATS 重复并交替版本顺序。`REVIEW_COMPARISON` 输出版本哈希、工具轨迹、原始结论及 `false_positive`、`false_negative`、未读取源码、无效结论或无来源引用等分类。基线缺陷作为对照数据；当前版本的评分失败或任一版本的接口错误使评测失败。

最终结论使用结构化 JSON，检查缺陷定位与源码引用。引用真实、定位正确不代表自然语言中的因果解释必然正确，仍需人工审阅原始结论。默认离线测试只验证成对场景的评分、回放、版本隔离和工具边界，不代表真实模型减少了误报或漏报。
