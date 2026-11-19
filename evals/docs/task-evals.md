# 任务评测与版本对照

`agent_eval` 是以真实 NekoCode 二进制为对象的黑盒评测器。用户任务、种子环境和独立验收固定，让 harness 自己决定实现步骤。现有 Go 提示词行为评测保持独立，本入口不替换它们。

当前有 5 个 case，`smoke` 保留原有 3 题，新增的 `visual` suite 覆盖视觉创作：

| Case | 覆盖能力 | 独立验收 |
|---|---|---|
| conduit-lite | 修复、产品交付、HTTP、状态不变性 | 9 项标准库探针，带 bug 初始版应失败 |
| snake | 产品实现、浏览器验证、状态与收尾 | 10 项真实 Chrome 操作，冻结时钟；含吃食物、自身碰撞、重开 |
| review-clean | 阴性审查、克制、证据说明 | 拒绝错误泄漏结论、原文件不变、结构化依据 |
| pelican-bicycle | SVG 创作、空间关系、视觉自检 | 4 项工程检查 + 4 项单独截图复核；见下文 |
| grafana-anonymous-review | 大型项目、super 多 agent 只读审查 | 结构/引用/只读自动检查，发现准确性独立裁定 |

提示词不规定 subagent 数量、技能或工具顺序。Snake 明确声明用于无障碍/外部验收的 DOM 接口；检查只覆盖清单，不能等同于完整人工体验评价。Review 的依据检查只核实结构与文件声明，不自动证明自然语言因果解释正确。

## 依赖与离线检查

基础运行需要 Python 3.11+、Git、编译好的 NekoCode headless；不引入 Python 第三方依赖。浏览器验收另外需要 Node、Chrome 与 `playwright-core@1.65.0`。浏览器依赖安装在评测环境，禁止复制到 agent 工作目录。`CHROME_BIN` 指定实际 Chrome 路径；`NODE_PATH` 指向安装了 playwright-core 的 node_modules。安装清单见 ../browser-package.json；不会在评测时自动联网安装。

```sh
cd evals
python3 -m agent_eval list
python3 -m agent_eval validate --suite smoke
python3 -m unittest discover -s tests -v
go test ./prompt_evals/...
```

默认测试不调用真实模型。开启浏览器自检：

```sh
NEKOCODE_EVAL_BROWSER_TESTS=1 python3 -m unittest discover -s tests -v
```

缺失浏览器依赖会使实际 case 未通过，不会被当作绿色通过；本地单元测试中的可选浏览器自检会明确 skip。

## 真实任务执行

从仓库根构建 `go build -o /var/tmp/nekocode-eval ./cmd/headless`。随后在 evals 中执行：

```sh
python3 -m agent_eval run \
  --case review-clean \
  --binary /var/tmp/nekocode-eval \
  --model deepseek/flash \
  --mode standard \
  --interaction-policy allow \
  --allow-user-config
```

去掉 `--case` 即执行 smoke suite；`--repeat 3` 每个 case 运行三次。`--mode super-agent` 测试协调模式，两版比较必须相同。`--output /absolute/results` 指定持久化目录，默认 evals/runs（Git 忽略）。所有写入命令的输出目录必须位于源码仓库外，或位于 Git 忽略且不含已跟踪文件的目录；校验在写入前执行，不自动修改忽略规则。已有未忽略的历史产物须先移出仓库或明确忽略。`--telemetry-root` 显式选择当前应用数据目录，默认 ~/.nekocode。

`--allow-user-config` 表示明确使用现有应用模型配置与凭据，可能加载已配置插件/技能/钩子，并执行产品代码。不会改全局模型或权限设置；选择模型只影响试验会话。应使用专用评测账号/配置；当前应用尚无独立 config-dir 启动入口，评测器不能保证与真实用户配置完全隔离。

交互策略固定进 receipt：默认 deny；allow 只逐次批准普通请求，显式 sandbox_mode=host 的升级仍拒绝；问题一律拒绝，不由评测器擅自猜答案。依赖审批的产品可能因此失败，这是所选策略下的结果，不能静默切到 host 得到通过。

每个 case 冻结 wall_seconds/model_calls/total_tokens/verifier_seconds。墙钟独立于模型响应；调用/token 上限基于已完成调用的本地计量，进行中的调用可能超额。计量不可用时无法强制 token/call 上限，墙钟仍生效，并报告未知指标。

## 保存契约

Case/Suite/验收器进 Git；每个 Run/Trial 使用唯一 ID，拒绝覆盖已有目录。Run 的 run.json、Trial 的 state.json 和执行中补齐的 receipt.json 是可更新的执行状态；已完成的原始日志、产物与结果不由 compare/reverify 覆盖。文件系统本身不是 WORM 存储，需要长期防篡改时由存储层提供保护。

Run 保存 run.json、report.md、source/config-receipt.json 和各 trial。run.json.source 只记录源码 commit、dirty 状态，不生成源码补丁或未跟踪文件归档。该记录不能重建未提交工作区，也不证明传入二进制由此 checkout 构建；二进制 SHA256 单独记录，二进制应自行留存。

本地 fixture 题目保存如下内容。仓库审查题不保留 workspace/product/验收源码副本，receipt.json.repository 保存 URL、固定 commit、diff 和输入树摘要；product_retained=false 的报告不能使用 reverify，只能重新执行固定版本的 case。

Trial 保存：

| 文件/目录 | 内容 |
|---|---|
| receipt.json | case/suite输入指纹、二进制、模型选择、模式、预算、配置和环境 |
| case/ | 冻结提示词、种子、验收脚本及所有辅助资源 |
| trace/protocol.jsonl | 双向 stream-json，含相对时间 |
| trace/calls.jsonl / trace/events.jsonl | 精确会话的物理模型计量与运行日志 |
| trace/observed.json | 实际模型选择、工具/扩展发现、会话绑定 |
| product/ | 结束后的产品快照，验收不在此目录执行 |
| verification/v1/results/ | 一次独立验收、stderr/stdout、截图与结果 |
| outcome.json / report.md | 机器结果和人读报告 |

执行与验收工作区使用临时目录，正常结束或异常退出后清理，只归档冻结题目和最终产品。原始轨迹与产品可能包含项目内容；默认只保存在本机。配置输入只保存 hash，不复制应用凭据。导出前自行选择需要公开的文件。

## 评分和完整性

产品完成度、执行是否正常收尾、观测完整性分别保存。success 要求运行 settled 且全部必需验收通过。harness 错误、超时和预算耗尽仍是交付失败；验收器自身失败/依赖缺失单独标记，不能当作模型错误得分或通过。

最终答案只采用 stream-json 的公开 result：包括原始任务，以及由公开 init 标记为 task_completion 的自动协调轮次；本地 journal 不补写或覆盖答案。后台任务与投递结算、全树调用计量仍由本 trial 会话的 session/runs 事件和 llm-calls.jsonl 轮转日志辅助观察，缺失日志不按 0 使用，也不会请求昂贵的 observe.overview。public_initial_answer_present 表示初始轮次是否返回文本，public_final_answer_present 表示最后公开结果是否有文本。

计量是“观察到的调用”，轮转/关闭日志可能漏计。执行器不自动评价有效推进或重复工具输入。人工分析可另存 annotation 文件，带 call/event ID，不改原始记录。

独立验收在 Linux Bubblewrap 私有挂载、PID、网络和 HOME 环境中执行。冻结题目和产品输入只读，普通产品在隔离临时副本中接受测试；宿主只导出普通判分文件和截图。Agent 执行阶段的权限与命名空间独立；验收仍要求受信任的验收脚本，不提供防止进程内篡改判分的保证。

## 比较版本

```sh
python3 -m agent_eval compare /results/run-baseline /results/run-candidate \
  --output /results/comparison-new
```

按 case ID+repeat 配对，同时校验完整 case指纹（含验收器辅助文件）、模型请求及实际观测模型/effort、评测契约版本、适配器版本、模式、交互策略、配置指纹、环境与预算。新增题显示 unmatched；改验收器/提示词/种子显示 incomparable。报告分新增成功、退步、双方成功、双方失败，不把不同题集总分直接排名。

成本只在双方成功的配对上比较，输出candidate-baseline中位数，每个指标带自己的有效样本数。缺失值是null，超时不因较少token获得效率优势。先看验收与退步，再看成本。

配置指纹默认哈希应用config/permissions以及skills/agents/hooks/rules/plugins；不保存其原文。外部MCP服务状态、环境注入和祖先项目配置不保证固定；操作者有更完整的有效配置清单时，可传入其SHA256作为--config-fingerprint。远程模型别名不能锁定供应商部署；比较前检查observed与calls中的实际model/effort，并优先在相近时间重跑基线。建议交替运行顺序，重要结论重复3–5对；单次结果不证明统计提升。

复验已有产物：

```sh
python3 -m agent_eval reverify /results/run/trials/trial-id --output /results/verification-new
```

新结果放新目录；reverify验证保存的case和product指纹，使用冻结验收器，拒绝悄悄换题。需要改变评分规则时先升级case版本，重跑正式比较。

## 添加 case

在 cases 下增加稳定 ID 目录，写 case.json、prompt.md 和 verify.py。本地题目提供 fixture；仓库审查题使用 fixture=null，并增加 repository={url, base, head}，base/head 必须为完整固定 commit SHA。case.json使用现有schema，不接受拼错/额外字段；requirements为唯一ID、description、required；verify.py接受workspace/output两个绝对路径，向output/result.json写schema_version=1和完整checks列表，每项id/status/evidence，status为passed/failed/not_run。验收未执行不得passed。schema_version和case.version是不同概念。

校验器支持多个语言的产品，但验收入口保持Python标准库，浏览器可委托固定Node脚本。所有辅助资源都参与case指纹。只向agent复制fixture与prompt，参考实现放tests/oracles，不放fixture。先验证初始失败、参考通过、表面修复仍失败；纯阴性审查例先验证无改动正确答案通过、误报/无关修改失败。再加入suite并升级suite版本。旧报告引用旧case/suite，不覆盖历史。

### 工具统计口径

`outcome.json` 的 `cost` 将工具行为分开保存：

- `tool_requests`：主 agent 在本次任务公开协议中发出的工具请求数，按 run ID + call ID 去重，排除设置轮次与子 agent 帧。
- `tool_result_errors`：同一协议范围内 `is_error=true` 的工具回执数。进程非零退出但工具正常返回不算在这里。
- `tool_started`、`tool_blocked`：当前会话本地运行日志记录的开始执行数、拦截数。范围与主 agent 协议不同，不能用两者相减推算拦截数。
- `tool_calls`：保留历史含义，等于 `tool_started`，兼容旧报告。
- `context_snapshot_polls`：评测器为确认运行/后台任务状态发出的 host `context.snapshot` 请求数；它是本地控制流量，不是模型调用或 token。预算与 usage 仍按 1 秒频率检查；状态轮询从 3 秒开始退避，最长 12 秒，收到最终任务答复后恢复 3 秒确认间隔。

缺失或不可解析的观测记为 `null`，不补零。工具内部自行发起的模型请求可能不在调用日志中，相关 token 开销仍是未知量。`evaluator_fingerprint` 保存完整评测器源码身份，用于追溯；`evaluation_contract_version` 决定执行、交付判定和计量口径是否兼容。改变这些语义时必须提升 storage.py 中的契约版本，展示或历史页面文案变化不需要升级。契约版本相同仍需满足 case、模型、预算、配置等既有比较条件。没有契约版本的旧记录标记为不可比较；口径改变后应重跑正式比较。

### 鹈鹕骑车：视觉与工程结果分开

来源为 [Simon Willison 的原始题目](https://github.com/simonw/pelican-bicycle)，保留英文首句，增加 `pelican.svg`、静态自包含矢量交付约束。单独执行：

```sh
NODE_PATH=/path/to/node_modules CHROME_BIN=/path/to/chrome \
python3 -m agent_eval run --case pelican-bicycle \
  --binary /var/tmp/nekocode-eval --model deepseek/flash \
  --mode standard --interaction-policy allow --allow-user-config
```

也可用 `--suite visual`。原来的默认 smoke 题集不变。

自动检查有效 SVG/viewBox、无活动或外部内容、无位图、真实 Chrome 桌面/窄屏非空渲染。保存 `verification/v1/results/desktop.png`、`verification/v1/results/mobile.png`。**自动 success 仅表示工程交付通过，不表示画对了鹈鹕骑车，也不表示审美通过。** 不用标签、ID、圆形数量或 agent 自述推断语义正确。

鹈鹕辨识、自行车结构、骑行动作和画面质量四项自动状态为 `not_run`，截图复核按 [review.md](../cases/pelican-bicycle/review.md) 另存 `visual-review-v1.json`。复核类型必须区分真人与助手看图，未知记录 uncertain；不改原 outcome。比较此 case 成本前，要先核对视觉复核结果，不能仅凭技术通过宣布能力提升。此版本不提供自动视觉金标准，视觉任务的全自动质量排名尚未支持。

### 图像调用计量与未知成本

新版本的 `image_understand` 尝试也进入物理模型调用日志，计入 `model_calls`；`tool_model_calls` 单列当前已接入计量的图像调用，`tool_call_id` 关联触发它的工具请求。它包含失败的模型尝试，不能再把模型调用数理解为仅主 agent 轮数。其他尚未接入日志的模型工具仍可能漏计。

`usage_unknown_calls` 表示供应商未返回 usage 的调用数。存在明确未知 usage 时，完整 token 总量为 `null`；`observed_total_tokens` 是已报告 token 的小计/下界。token 预算仍约束这个下界，模型调用数和墙钟预算照常生效；未知开销不能被保证在预算内，也不能按零参与成本比较。

此轮变更改变了 evaluator fingerprint。历史运行保留原口径，旧版未记录的图像调用不能事后凭空补齐；单次对照应明确标注不可严格配对。


## 目录版本与历史入口

layout_version=2 的 trial：原始记录在 trace/，验收输出在 verification/v1/results/。case/ 是冻结题目，product/ 是最终快照；执行和验收源码副本在系统临时目录，结束后清理。仓库审查不保留源码产品。Run 的源码身份和配置指纹记录在 run.json.source 和 source/config-receipt.json。

仅支持当前格式复验，旧格式报告直接查看，重新评测应运行当前 case。人工视觉或审查复核可另存 analysis/ 下的 JSON，注明复核者、产品指纹和依据；执行器不分配人工分析版本。

```sh
python3 -m agent_eval history --case pelican-bicycle
python3 -m agent_eval history --refresh
python3 -m agent_eval reverify runs/<run-id>/trials/<trial-id>
python3 -m agent_eval compare runs/<baseline-id> runs/<candidate-id>
```

history 默认只读元数据；--refresh 更新 runs/index.json 和 index.md，它们只是可再生成的导航。执行 case 不自动更新索引；损坏或未完成记录不会被标成成功。

reverify 不带 --output 时追加到 trial 的 verification/vN，输出 results/ 和 source.json；验收工作区在临时目录清理，不覆盖原始结果。仓库审查只保留报告，不能离线 reverify，需要重新运行。compare 默认写 comparisons/<unique-id>/，可显式 --output。run 可加 --label 便于识别，label 不替代唯一 ID。

cases、suites、执行器、测试、文档进 Git；runs 与 comparisons 是本地运行数据，Git 忽略。产物和历史保存在同一份 run 档案，不复制 history/latest。根目录 go.mod 保留模块边界，防止生成 Go 项目被主应用 go test ./... 扫描。

### 大型仓库的 super 审查

`grafana-anonymous-review` 属于 `review-large`。直接以 `--case grafana-anonymous-review --mode super-agent` 执行；case.json.repository 固定仓库地址及 base/head。只有选择执行该题时，才在系统临时目录浅拉取源码、检出 head 并生成 review.patch；list/validate 不联网。准备失败单独记为 preparation_failed，不启动 Agent，日志在 trace/preparation.log。临时工作区在验收结束后清理，不复制进冻结 case、product 或验收副本，trial 仅保留报告、轨迹、验收与版本摘要。完整说明见 [case README](../cases/grafana-anonymous-review/README.md)。

此 case 的自动 success 只表示结构化审查交付与只读契约通过；准确率和召回需按 case/review.md 外部逐条裁定。评估多 Agent 协作时，receipt 中的模式必须为 super-agent；standard 可作单 Agent 基线，success 本身不认证协作模式。参考答案不放 fixture。不要把任务数、多 agent 并发率或更多发现数直接当成质量提高。


### 调用预算与阶段交接

执行器通过 stream-json initialize 的 `model_call_budget` 把 case 的调用上限传给支持该选项的 NekoCode。`outcome.json.execution_budget` 记录最近一次快照（可能早于最后一个请求），原始协议保留每次观测；成本仍以调用日志为准。原生 admission 包含在途请求、失败、重试、图像理解和压缩，并为 worker 交接及协调汇总预留容量。达到恰好上限时让已受理的工作在配置的墙钟预算内收尾；超限、无原生预算支持或无法收尾时仍判预算失败。Token/wall 上限仍为外部兜底，token 仅有可观测小计，不能保证不超限。

Super 子任务默认每 12 个普通模型轮次交接阶段结果；partial 需由协调者判断是否 resume 原任务。这会增加必要的交接与协调成本，应比较最终完成度、有效发现、总调用与 token，不能只看每个 worker 的轮数。旧基线和新执行器的计量/预算机制不同，比较时须披露 evaluator fingerprint 的变化，不覆盖旧记录。

### 审查过程记录

新运行在 `trace/tasks.json` 冻结后台任务和交接快照；未取得状态时显式 available=false。`outcome.json.coordination` v2 统计全树 runtime 工具错误、task_result 读取错误、通用 handoff outcome 分布；这些是过程指标，不代表成果正确。不再统计已退役的 findings/disposition 审查清单。旧运行不补写或覆盖；准确率仍由 case 的独立验收器或 adjudication 判断。
