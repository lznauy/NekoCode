# NekoCode Evals

用固定任务、真实执行轨迹和独立验收，检查 NekoCode 能否交付产品，并比较不同版本的结果、模型调用和 token 成本。

这里有两条评测路径：`agent_eval/` 通过 headless 二进制执行完整任务，本文主要介绍这条路径；`prompt_evals/` 是独立的 Go 提示词行为评测，使用方法见 [prompt_evals/README.md](prompt_evals/README.md)。

## 目录结构

```text
evals/
├── agent_eval/          # Python 命令入口、执行、计量、验收与比较
├── cases/               # 题目定义、提示词、初始项目和独立验收器
├── suites/              # 一组题目的选择清单
├── tests/               # 评测器自身的离线测试
│   └── oracles/         # 验收器自检用的参考产品，不提供给 Agent
├── prompt_evals/        # Go 提示词行为评测
├── docs/                # 保存契约、计量与扩展说明
├── browser-package.json # 可选浏览器验收依赖
├── go.mod               # 独立 Go 模块边界
├── runs/                # 运行后生成：每次运行的产物和证据，Git 忽略
└── comparisons/         # 比较后生成：版本对照报告，Git 忽略
```

每道题放在 `cases/<case-id>/`；本地题目提供 fixture，仓库审查题在 `case.json.repository` 中记录地址及固定 base/head，并将 fixture 设为 null：

| 文件或目录 | 用途 |
|---|---|
| `case.json` | case 身份、能力标签、预算、验收项和资源路径 |
| `prompt.md` | 交给 Agent 的任务要求 |
| `fixture/` | 复制进工作区的初始项目 |
| `verify.py` | 独立验收入口 |
| `browser.cjs`、`acceptance.py` 等 | 各题需要的辅助验收脚本 |
| `review.md` | 部分题目的人工复核标准 |

当前题目与套件：

| Case | 任务 | 额外准备 | 所属 suite |
|---|---|---|---|
| `review-clean` | 沿调用链核实文件句柄泄漏，检查误报与无关修改 | 无浏览器依赖 | `smoke` |
| `conduit-lite` | 修复并交付 Python 文章浏览产品 | 验收会启动本地 HTTP 服务 | `smoke` |
| `snake` | 实现并验证贪吃蛇网页 | Node、Chrome、Playwright | `smoke` |
| `pelican-bicycle` | 绘制鹈鹕骑自行车的 SVG | Node、Chrome、Playwright；画面需要另行复核 | `visual` |
| `grafana-anonymous-review` | 大型 Grafana 仓库的只读审查 | 执行时拉取固定版本，使用 `super-agent` 模式 | `review-large` |

## 环境准备

以下命令按 Bash 和 Linux 环境编写。Python 执行器使用 POSIX 进程组管理，Windows 建议在 WSL 中执行。

基础依赖为 Python 3.11+、Git，以及可以构建本仓库的 Go 工具链，Go 版本以仓库根 `go.mod` 为准。Python 执行器只使用标准库。真实任务还需要在 NekoCode 应用中配置可用模型；下面的 `deepseek/flash` 只是示例，应替换为本机已配置的模型选择名。

在**仓库根目录**构建：

```sh
go build -o /tmp/nekocode-eval ./cmd/headless
cd evals
```

**除明确标注外，后续命令都在 `evals/` 中执行。** 从仓库根直接运行 `python3 -m agent_eval` 会找不到模块。

查看命令和校验默认题集，这些操作不会调用模型：

```sh
python3 -m agent_eval --help
python3 -m agent_eval run --help
python3 -m agent_eval validate --suite smoke
python3 -m agent_eval list
```

`list` 和 `validate` 只检查本地题目定义，不下载仓库源码。Grafana 等仓库审查题只保存 GitHub 地址与固定 commit，选择该 case 执行时才拉取；其他题目使用随 case 提供的小型初始项目。

### 浏览器题目的依赖

`snake` 和 `pelican-bicycle` 的独立验收需要 Node、Chrome/Chromium 和 `playwright-core`。依赖版本由 [browser-package.json](browser-package.json) 固定；评测运行不会自动下载安装。

下面将 Node 依赖安装到仓库外，避免复制进 Agent 的初始项目：

```sh
# 在 evals/ 中执行，需要联网下载 npm 包。
eval_browser_dir="${XDG_CACHE_HOME:-$HOME/.cache}/nekocode-eval-browser"
mkdir -p "$eval_browser_dir"
cp browser-package.json "$eval_browser_dir/package.json"
npm install --prefix "$eval_browser_dir"
export NODE_PATH="$eval_browser_dir/node_modules"

# 已安装 google-chrome 时可这样指定；使用 Chromium 时改为对应可执行文件。
export CHROME_BIN="$(command -v google-chrome)"

node -e 'console.log(require("playwright-core/package.json").version)'
"$CHROME_BIN" --version
```

`playwright-core` 不附带浏览器，Chrome/Chromium 需要提前安装。自动验收会启动浏览器并保存截图；只有执行下面的网页预览步骤时，才需要自己访问页面。

## 执行一个 case

### 先运行不需要浏览器的题目

```sh
python3 -m agent_eval run \
  --case review-clean \
  --binary /tmp/nekocode-eval \
  --model deepseek/flash \
  --mode standard \
  --interaction-policy allow \
  --allow-user-config \
  --label review-clean-current
```

这会产生真实模型调用。`--allow-user-config` 表示明确允许使用本机应用的模型配置与凭据，也可能加载已配置的插件、技能和钩子；它不代表隔离配置环境。`--model` 只选择本次试验会话的模型。

终端会先打印新建的运行目录，例如：

```text
/your/repo/evals/runs/run-20261115T100000Z-0123456789
```

随后按 trial 打印 `success`、`stop` 等结果。保存这个目录路径，后续从中查看报告和产物。每次执行都会生成新的唯一 ID，`--label` 只是方便识别的标签，不会覆盖同名历史。

### 执行产品、视觉或大型审查题

已准备浏览器依赖后，执行贪吃蛇：

```sh
python3 -m agent_eval run --case snake \
  --binary /tmp/nekocode-eval --model deepseek/flash \
  --mode standard --interaction-policy allow --allow-user-config \
  --label snake-current
```

执行 SVG 视觉题：

```sh
python3 -m agent_eval run --case pelican-bicycle \
  --binary /tmp/nekocode-eval --model deepseek/flash \
  --mode standard --interaction-policy allow --allow-user-config \
  --label pelican-current
```

Grafana 无需提前准备源码，直接在 `evals/` 执行：

```sh
python3 -m agent_eval run --case grafana-anonymous-review \
  --binary /tmp/nekocode-eval --model deepseek/flash \
  --mode super-agent --interaction-policy allow --allow-user-config \
  --label grafana-current
```

该命令在系统临时目录浅拉取固定 base/head，检出源码并生成审查 diff，验收结束后清理。每次 trial 独立准备，不保留常驻源码缓存；拉取失败记录为 `preparation_failed`，不启动 Agent。题目的来源、只读约束和准确性复核见 [Grafana case README](cases/grafana-anonymous-review/README.md)。

### 选择多道题、套件或重复次数

```sh
# 只运行列出的两道题。
python3 -m agent_eval run --case review-clean --case conduit-lite \
  --binary /tmp/nekocode-eval --model deepseek/flash \
  --mode standard --interaction-policy allow --allow-user-config

# 执行 smoke 的三道题，每道重复三次；包含 snake，需要浏览器依赖。
python3 -m agent_eval run --suite smoke --repeat 3 \
  --binary /tmp/nekocode-eval --model deepseek/flash \
  --mode standard --interaction-policy allow --allow-user-config
```

省略 `--case` 和 `--suite` 时运行默认 `smoke`。`--case` 可重复指定，并优先于 `--suite`；`--repeat` 允许 1–5，默认 1。一次命令是一个 run，每道题每次重复是一个 trial，目前按重复轮次与题目顺序串行执行；`super-agent` 内部的任务调度由 NekoCode 自己负责。

常用参数：

| 参数 | 默认值或含义 |
|---|---|
| `--binary` | 必填，已构建的可执行文件，建议使用绝对路径 |
| `--model` | 必填，本机应用中的模型选择名 |
| `--mode` | 默认 `standard`，也可选 `super-agent` |
| `--interaction-policy` | 默认 `deny`；示例使用 `allow`，逐次批准普通工具审批 |
| `--allow-user-config` | 真实执行必填，确认使用本机应用配置 |
| `--output` | 默认 `evals/runs`；也可指定如 `/tmp/nekocode-eval-runs` |
| `--telemetry-root` | 默认 `~/.nekocode`，用于读取应用配置指纹和会话计量；不会替应用切换配置目录 |
| `--label` | 可选，便于在历史中识别运行 |
| `--config-fingerprint` | 可选，有效外部配置的 SHA256，不是密钥或配置原文 |

`allow` 仍拒绝显式 `sandbox_mode=host` 升级，问题请求一律拒绝。需要人工回答的任务可能因此受阻，所选策略会记录在试验中。

墙钟、模型调用、token 和验收时间预算定义在各题的 `case.json`。仓库准备在 Agent 启动前执行，每条 Git 命令最多 120 秒，准备耗时单独记录在 `receipt.json.preparation_elapsed_s`。支持原生调用预算的二进制会限制整个执行链的调用准入，包括在途请求、重试、压缩和已接入的图像调用，并预留交接与汇总额度。token 上限依赖已观测用量，可能超额；未报告的用量不能视为零。

## 查看报告和产物

### 从运行报告定位具体 trial

在下面两行中填入终端输出的 run 目录，以及其中实际存在的 trial 目录：

```sh
eval_run_dir="$PWD/runs/<run-id>"
cat "$eval_run_dir/report.md"
python3 -m json.tool "$eval_run_dir/summary.json"
find "$eval_run_dir/trials" -mindepth 1 -maxdepth 1 -type d

eval_trial_dir="$eval_run_dir/trials/<trial-id>"
cat "$eval_trial_dir/report.md"
cat "$eval_trial_dir/answer.txt"
python3 -m json.tool "$eval_trial_dir/outcome.json"
```

`report.md` 可以用编辑器的 Markdown 预览打开，trial 报告中的链接可直接定位产品、轨迹和验收结果。`summary.json` 是整次运行的简表，`outcome.json` 则包含单次试验的结果、成本、完整性和错误。若命令被中断，部分汇总文件可能尚未生成，先看 `run.json` 和已创建的 trial。

新版运行目录：

```text
runs/<run-id>/
├── run.json                        # 题集、标签、trial 清单和运行状态
├── summary.json                    # 运行正常结束后生成的结果汇总
├── report.md                       # 整次运行的人读报告
├── source/                         # 配置指纹；run.json 记录源码 commit 与 dirty 状态
└── trials/<trial-id>/
    ├── receipt.json                # 题目、二进制、模型、配置、环境和预算
    ├── state.json                  # trial 执行状态
    ├── case/                       # 冻结的题目与验收资源
    ├── product/                    # 验收前冻结的最终产品
    ├── answer.txt                  # 最后一次公开结果文本
    ├── outcome.json                # 成功、结束原因、成本和验收等机器结果
    ├── report.md                   # 单次试验报告及产物链接
    ├── trace/
    │   ├── preparation.log        # 输入准备日志，含 Git 初始化与仓库拉取
    │   ├── protocol.jsonl          # 双向协议记录
    │   ├── calls.jsonl             # 已观测物理模型调用
    │   ├── events.jsonl            # 运行事件
    │   ├── observed.json          # 模型、工具与扩展的实际观测
    │   ├── tasks.json             # 后台任务与交接快照
    │   └── stderr.log             # headless 进程的标准错误
    └── verification/v1/results/   # 验收 JSON、日志，以及部分题目的截图
```

本地题目保留冻结的 `case/` 和最终 `product/`，执行与验收工作区位于系统临时目录，完成或异常退出时清理。冻结题目包含初始 fixture，供复验使用；验收在产品的临时副本中执行，避免修改归档产品。强制终止或断电可能留下系统临时文件。

仓库审查题只保存报告、轨迹、验收结果及 `receipt.json.repository` 中的地址、固定 commit 和输入摘要，`outcome.json.product_retained=false`，不保存源码产品。

临时工作区不继承原 `evals/runs` 上级的项目配置，仍使用应用的用户配置和题目自身文件。产品须能从归档目录独立运行，避免引用临时目录的绝对路径。此次执行契约版本为 3，不能与旧口径运行直接比较。

后续复验会检查产品指纹，直接编辑 `product/` 会导致复验拒绝。手动调试产品时先复制到其他目录。

### 查看网页、SVG 和截图

静态网页或 SVG 可以用本地 HTTP 服务预览：

```sh
python3 -m http.server 8765 --bind 127.0.0.1 \
  --directory "$eval_trial_dir/product"
```

在浏览器打开 `http://127.0.0.1:8765/` 查看静态网页，或打开 `http://127.0.0.1:8765/pelican.svg` 查看 SVG。服务占用当前终端，用 Ctrl+C 停止。

Conduit 是带 API 的 Python 产品，应按 `answer.txt` 或产品内的运行说明启动；沿用题目接口时，入口为 `python3 app.py --port 8080`，运行目录应是产品的调试副本。单纯的静态 HTTP 服务不能提供它的 API。

自动验收截图直接用图片查看器打开：

| Case | 首次验收截图 |
|---|---|
| `snake` | `verification/v1/results/product.png` |
| `pelican-bicycle` | `verification/v1/results/desktop.png`、`mobile.png` |

只有验收执行到截图步骤时，文件才会生成。`pelican-bicycle` 的自动成功只说明 SVG 工程约束和渲染检查通过；是否画对、画得如何，应按 [视觉复核规则](cases/pelican-bicycle/review.md) 检查截图，另存复核结果。Grafana 的自动成功也只覆盖审查交付结构、引用和只读约束，发现是否准确需按它的 `review.md` 单独裁定。

### 排查失败和成本

建议按以下顺序查看：

1. `outcome.json` 的 `stop_reason`、`error` 和 `verification`，区分准备失败、运行失败、预算耗尽和验收失败。`preparation_failed` 时先看 `trace/preparation.log`，此时还没有模型调用。
2. `verification/v1/results/verification.json` 查看归一化验收结果；`result.json` 是验收器原始输出，可能在异常时缺失。
3. 同目录 `stdout.log`、`stderr.log` 查看验收日志；`trace/stderr.log` 查看应用进程错误。
4. `answer.txt` 与 `trace/protocol.jsonl` 查看公开答复及工具交互，`trace/tasks.json` 查看后台任务与交接状态。
5. `trace/calls.jsonl` 查看模型调用与 token，`trace/events.jsonl` 查看工具执行事件。

`outcome.json.success` 要求执行正常收尾，并通过全部必需验收。`verification.product_passed` 只表示产品验收通过；超时前已经写出可用文件，也可能仍是交付失败。`state.json` 的 `finished` 表示执行器已写完本次结果，不等于评测成功。

`cost.total_tokens=null` 表示完整用量未知；`observed_total_tokens` 是已知小计或下界。`model_calls` 包含已接入计量的图像理解等工具内部调用。日志关闭或轮转可能漏计，需同时查看 `integrity`，不能把未知值当作零。运行命令退出码为 0 表示全部 trial 成功，1 表示至少一项失败；参数或输入错误通常返回 2。

## 浏览历史和复验

```sh
python3 -m agent_eval history --limit 20
python3 -m agent_eval history --case snake --limit 10
python3 -m agent_eval history --refresh
```

`history` 默认读取 `runs/` 的元数据，`--refresh` 生成 `runs/index.md` 和 `index.json`，可打开索引中的报告与产品链接。用了外部输出目录时，改为 `history --root /tmp/nekocode-eval-runs --refresh`。尚未运行时没有历史目录是正常情况。

复验保留了产品的本地题目，不调用模型：

```sh
python3 -m agent_eval reverify "$eval_trial_dir"
```

仓库审查题只保留报告，`reverify` 会明确拒绝，因为原源码工作区已清理。查看已有报告无需下载；再次调用 Agent 审查时重新执行固定版本的 case。

`reverify` 使用冻结的题目和验收脚本，先检查 case 与产品指纹，再在临时产品副本上验收，结果追加到 `verification/vN/results/`，结束后清理副本。它可能执行产品代码或启动浏览器，需要原来的验收依赖。复验不覆盖初次结果，也不改写原 `outcome.json`。

也可用 `--output` 指定一个尚不存在的新目录；trial 内只允许直接新建 `verification/vN`，不能嵌入已有验收证据。直接 `verify` 的输出也不得与待验收目录重叠。输出位置须在源码仓库外，或位于 Git 忽略且不含已跟踪文件的目录。仅支持当前 `layout_version=2` 的复验；更早的报告可直接查看，重新评测应运行当前 case。

历史索引仅在显式执行 `history --refresh` 时生成，运行 case 不会重扫全部历史。执行器不再提供独立的自动分析命令；排查直接查看报告与原始轨迹，人工复核可另存 `analysis/` 文件，注明依据与产品指纹。

## 比较两个版本

分别构建并保留基线与候选二进制，用相同的 case、模型、模式、交互策略、环境和预算执行。比较时传入两个 **run 目录**：

```sh
python3 -m agent_eval compare \
  "runs/<baseline-run-id>" "runs/<candidate-run-id>"
```

默认写入新的 `comparisons/<comparison-id>/`，打开其中的 `report.md`，机器结果在 `comparison.json`。也可以指定 `--output /tmp/nekocode-eval-comparison-new`。

比较按 case ID 与重复序号配对。缺少题目显示 `unmatched`，题目、模型、配置或评测契约等条件不同会显示 `incomparable`。先看 `regression`、`new_success`、`both_success` 和 `both_failure`；成本只对双方都成功且具有必要计量的配对计算差值。少量样本不足以证明稳定提升，正式结论建议重复 3–5 次并人工复核产物。

## 验证评测器本身

以下默认检查不调用真实模型：

```sh
python3 -m agent_eval validate --suite smoke
NEKOCODE_EVAL_BROWSER_TESTS=0 python3 -m unittest discover -s tests -v
go test ./prompt_evals/...
```

启用可选的真实浏览器自检：

```sh
NEKOCODE_EVAL_BROWSER_TESTS=1 python3 -m unittest discover -s tests -v
```

部分自检会绑定本地 HTTP 端口，在禁止本地监听的沙箱中会失败。`NEKOCODE_EVAL_BROWSER_TESTS` 只控制单元测试中的可选浏览器自检，不会让真实 `snake` 或 `pelican-bicycle` 任务跳过浏览器验收。

`tests/oracles/` 的参考产品只用于检查验收器能否接受正确产品、拒绝缺陷变体，不会复制到 Agent 工作区，也不计作模型成绩。

## 修改题目与数据保存边界

修改提示词去 `cases/<case-id>/prompt.md`，修改初始项目去 `fixture/`，修改验收要求去 `case.json` 与 `verify.py`，修改题集去 `suites/`。题目、种子和验收辅助资源都参与指纹；修改后重跑正式比较，不能用换过规则的新结果覆盖旧成绩。添加 case 的字段、验收输出协议和计量口径见 [详细说明](docs/task-evals.md)。

cases、suites、执行器、测试与文档进 Git；runs 与 comparisons 是本地运行档案，Git 忽略。没有额外复制的 history/latest 目录，历史索引可从 run 元数据重建。

运行只记录当前源码 commit、dirty 状态和二进制摘要，不自动打包项目源码；dirty 状态无法重建工作区，也不能证明二进制来自该 checkout。配置指纹不复制配置原文。原始协议与产品仍可能含项目内容。分享运行目录前检查具体文件。Agent 与验收器运行在同一宿主用户下，验收副本用于保护原产品，不能视为安全隔离环境。
