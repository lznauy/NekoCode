# Grafana 大型仓库审查

审查 Grafana PR #79265 的前两个提交。仓库地址和固定 base/head commit 写在 `case.json.repository`，case 目录只保存题目、版本与验收规则。

从仓库根构建，然后进入 evals：

```sh
go build -o /tmp/nekocode-eval ./cmd/headless
cd evals
python3 -m agent_eval run --case grafana-anonymous-review \
  --binary /tmp/nekocode-eval --model deepseek/flash \
  --mode super-agent --interaction-policy allow --allow-user-config \
  --label grafana-super-review
```

执行该 case 时才联网浅拉取固定的两个 commit，在系统临时目录检出 head 并生成 `review.patch`。传给 Agent 的是源码树和差异，不含上游 Git 历史与远端。`list`、`validate` 和执行其他 case 均不下载源码，没有单独的准备脚本或常驻缓存。

准备阶段每条 Git 命令最多等待 120 秒，日志写入 `trace/preparation.log`；失败记录为 `preparation_failed`，Agent 不启动，验收项为 `not_run`。准备耗时单独记录，不消耗模型调用额度。Agent 执行预算为 900 秒、100 次全树调用和 300 万已观测 token。

验收在临时源码树清理前执行，用执行前的摘要检查源码、差异和文件集合是否改变，同时检查报告结构与引用行号。trial 保存 `answer.txt`、轨迹、验收结果和 `receipt.json` 中的仓库地址、base/head、diff 摘要与输入树摘要，不保存完整源码、工作区或产品副本。结束或异常退出时清理临时目录；强制终止或机器断电可能留下系统临时文件。

这类只保留报告的运行没有可供离线复验的产品，`reverify` 会明确拒绝。查看已有结论直接打开报告；需要再次让 Agent 审查时重新运行同一固定版本的 case。

自动 success 只表示结构化审查交付、引用和只读契约通过，不代表发现准确。`review_accuracy` 始终为 `not_run`，按 [review.md](review.md) 另存 `analysis/vN/adjudication.json`，核实参考、额外发现、重复项和污染风险。过程中的任务数量或并行度不能直接当作质量提高。

本机同宿主运行不能强隔离答案与网络；发现读取评测侧材料、公开 issue 或修复的调用时，应单独标记污染。公开样本也可能已被模型训练见过，不能将单题结果泛化为未见项目能力。
