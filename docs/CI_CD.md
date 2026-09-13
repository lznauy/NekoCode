# NekoCode CI/CD 流程

本文档面向项目维护者，说明代码从分支进入 `master`，再成为 GitHub Release 的完整路径。日常检查由 [ci.yml](../.github/workflows/ci.yml) 按改动范围编排，可复用检查按 Go、GUI、官网、仓库质量和文档拆分在 [`.github/workflows`](../.github/workflows) 中；发布编排以 [release.yml](../.github/workflows/release.yml) 为准。本地发布步骤见 [RELEASING.md](RELEASING.md)。

## 流程概览

```text
开发分支或 Pull Request
          │
          ▼
      路径感知 CI
          │
          ├── Go 变化：格式、构建、测试、vet、race、漏洞检查
          ├── GUI 变化：测试与前端构建
          ├── 官网变化：lint 与构建
          ├── 脚本/CI 变化：ShellCheck、安装器测试
          └── 文档变化：链接与 fragment 检查
          │
          ▼
更新 CHANGELOG，创建 annotated SemVer tag
          │
          ▼
      Release workflow
          │
          ├── 校验 tag 与 CHANGELOG
          ├── 复用 master CI 做第二次完整验证
          ├── 构建 Linux/macOS 的 amd64/arm64 二进制
          └── 生成校验和、SBOM 与构建证明
          │
          ▼
      GitHub Release
          │
          ▼
下载校验、安装器验证、版本检查
```

## 分支与触发条件

CI 编排工作流在以下场景运行，并在内部按路径选择检查：

- push 到 `master`；
- 创建或更新目标为 `master` 的 Pull Request；
- Release workflow 通过 `workflow_call` 调用同一编排入口，并强制复用全部检查。

push 到 `dev` 不会直接触发 CI。发布前需要让待发布提交进入 `master`，并确认该提交的检查全部通过。

同一 workflow 和 Git ref 只保留最新任务。新提交到达后，仍在运行的旧任务会被取消，避免旧结果晚于新结果完成。`CI / gate` 始终运行并汇总所有已选择检查，适合作为分支保护的 required status check；不要把可能被路径分类跳过的子检查单独设为 required。

Pull Request 的路径分类器从目标分支对应提交读取，不执行 PR 自己修改后的分类器；否则 PR 可以把所有类别伪装成未变化。分类器尚未存在于目标分支、基线不可用或 Release 明确要求全量验证时，编排器采用 fail-safe 策略运行全部检查。gate 会要求每个被选择的检查明确返回成功，不把意外的 skipped 状态当作通过。

路径分流只优化日常开发，不降低发布门禁。典型触发关系如下：

| 变化范围 | 运行的检查 |
| --- | --- |
| `*.go`、`go.mod`、`go.sum`、Go 构建配置、`//go:embed` 资源 | Go core |
| `interaction/gui/web`、`wails.json` | GUI frontend |
| `official` 下非 Markdown 文件 | Website |
| `.github`、`scripts` | Repository quality |
| 任意 Markdown | Documentation |

一次提交涉及多个范围时，对应工作流会并行运行。修改某个子 workflow 本身会触发该类别及仓库质量检查；普通 Markdown 改动不会启动 Go、GUI 或官网构建。被 `//go:embed` 编译进二进制的提示词和 GUI `dist` 文件属于运行时资源，因此仍会触发 Go 检查。路径分类失败或任何已选择检查失败时，稳定的 `CI / gate` 都会失败。

## 路径感知 CI

CI 包含五类独立检查。日常提交只运行受改动影响的类别；正式 Release 会运行全部类别。任意已触发的检查失败，提交都不应进入发布阶段。

| Job | 检查内容 |
| --- | --- |
| `go` | `gofmt`、`go build`、`go test`、`go vet`、race detector、`govulncheck` |
| `frontend` | GUI 的依赖安装、Vitest 测试、TypeScript/Vite 构建 |
| `website` | 官网的依赖安装、ESLint、Next.js 构建 |
| `repository-quality` | Actionlint、ShellCheck、安装器校验测试 |
| `documentation` | 离线文档链接和 fragment 检查 |

### Go 与 CGO

Go 版本从 [`go.mod`](../go.mod) 读取，CI 不单独维护版本号。CGO 由固定版本的 Zig 编译，Linux amd64 验证使用 `x86_64-linux-gnu` 目标。

不要把 CI 编译目标改成 `native`。GitHub runner 的 CPU 型号可能不同，而 Go build cache 会跨任务恢复；宿主专用指令一旦被另一台 runner 复用，测试会批量出现 `signal: illegal instruction`。固定目标也让本地复现和 Release 构建使用相同的指令集基线。

race detector 覆盖并发密集的 `bot/...` 和 `runtime/...`。`govulncheck` 检查代码可达的漏洞调用链；如果标准库存在可达漏洞，应先升级 `go.mod` 中的补丁版本，再发布。

### GUI、官网与仓库质量

GUI 和官网使用各自的 lockfile 执行 `npm ci`，避免 CI 隐式更新依赖。GUI 当前只做测试和构建，不生成桌面安装包；官网只验证生产构建，不负责部署。

仓库质量检查覆盖 GitHub Actions 语法、`scripts/` 下的 Shell 脚本和安装器的校验失败路径。文档工作流独立检查 Markdown 内的本地链接、HTML 链接和标题锚点；新增发布入口或改名文档时，不需要运行完整产品构建，也能阻止失效链接进入 `master`。

## 发布准备

发布从干净且已通过 CI 的 `master` 开始。发布负责人需要：

1. 将 `docs/CHANGELOG.md` 中待发布的内容整理到版本章节，例如 `## v0.4.3 - 2026-08-09`；
2. 记录不兼容改动和迁移方法；
3. 按 [CONTRIBUTING.md](CONTRIBUTING.md) 运行本地验证；
4. 在至少一个支持的平台启动 TUI；
5. 用临时目录验证安装器；
6. 创建并推送 annotated tag。

```bash
git tag -a v0.4.3 -m "NekoCode v0.4.3"
git push origin v0.4.3
```

正式版本 tag 使用 `vX.Y.Z` SemVer 格式。不要用 lightweight tag。

## Release 门禁

push `v*` tag 会触发 Release workflow。`validate-tag` 在构建前检查：

- tag 符合完整 SemVer；
- `origin` 同时存在 tag object 和 peeled `^{}` commit，证明它是 annotated tag；
- tag 指向当前 workflow checkout 的提交；
- `docs/CHANGELOG.md` 有同名版本章节；
- 章节日期采用 `YYYY-MM-DD` 格式。

tag 校验通过后，Release workflow 会通过 `workflow_call` 再次调用 Go、GUI、官网、仓库质量和文档五套检查。该调用不受路径过滤影响；master 曾经通过不代表可以跳过这一步，tag 对应的提交必须独立通过全部检查。

## 构建矩阵与版本注入

Release 构建以下二进制：

| 系统 | 架构 | 文件名 |
| --- | --- | --- |
| Linux | amd64 | `nekocode-tui-linux-amd64` |
| Linux | arm64 | `nekocode-tui-linux-arm64` |
| macOS | amd64 | `nekocode-tui-darwin-amd64` |
| macOS | arm64 | `nekocode-tui-darwin-arm64` |

Linux 使用 Zig 的 `x86_64-linux-gnu` 和 `aarch64-linux-gnu` 目标交叉编译。macOS 在对应的 GitHub macOS runner 上构建。

构建命令启用 `-trimpath`，并通过 `-ldflags` 将 tag 写入 `nekocode/util/version.Version`。本地未注入版本时显示 `dev`，正式二进制显示对应的 Release tag。四个平台的中间 artifact 保留七天，只用于组装 Release。

## Release 产物与权限

四个平台构建全部成功后，`release` Job 会确认二进制数量，生成软件物料清单（SBOM）和 SHA-256 清单，再为发布文件生成 GitHub build provenance attestation。

GitHub Release 包含：

```text
nekocode-tui-linux-amd64
nekocode-tui-linux-arm64
nekocode-tui-darwin-amd64
nekocode-tui-darwin-arm64
sbom.spdx.json
SHA256SUMS
```

Release Notes 由 GitHub 根据合并记录生成，发布后仍需人工检查。GitHub Actions 均固定到 commit SHA；普通 CI 只有仓库只读权限，只有最终 `release` Job 获得 Release 写权限、OIDC token 和 attestation 写权限。

## 安装与更新

[`scripts/install.sh`](../scripts/install.sh) 支持 Linux 和 macOS 的 amd64、arm64。安装器先把二进制下载到目标目录内的随机临时文件，再读取 `SHA256SUMS` 并校验摘要。校验成功后才设置执行权限并移动到最终路径，失败不会覆盖已有安装。

```bash
sh scripts/install.sh --version v0.4.3 --dir /tmp/nekocode-install
```

默认安装 latest Release。指定版本用于回归验证和固定部署。v0.4.2 没有发布 SHA256SUMS，因此安装器保留该版本的内置摘要；新版本必须提供 Release 校验清单，缺失时安装器终止。

## 发布后验证

workflow 成功后，发布负责人还需要完成以下检查：

1. Release 中恰好存在四个二进制、SBOM 和 SHA256SUMS；
2. 下载全部受校验资产，执行 `sha256sum -c SHA256SUMS`；
3. 用 `scripts/install.sh --version <tag> --dir <临时目录>` 完成一次真实安装；
4. 用 `go version -m` 检查 Go 工具链和模块版本；
5. 启动 TUI，确认界面显示的版本与 tag 一致；
6. 检查生成的 Release Notes 和 provenance。

## 失败处理

master CI 失败时，不创建 tag。Release 的 tag 校验、完整 CI 或平台构建失败时，不应手工绕过门禁或单独上传缺失资产。

如果 workflow 在创建 GitHub Release 之前失败，可以修复提交、重新通过 master CI，并在确认同名 Release 和资产均不存在后重建失败 tag。Release 一旦公开，不得移动 tag 或替换原有资产；产物错误时应删除整次 Release，修复后发布新的补丁版本。

## 当前边界

当前 CD 只负责 GitHub Release，不包含：

- 官网生产部署；
- GUI 桌面安装包；
- Flatpak、AppImage、DMG 或容器镜像；
- 灰度发布、环境晋级和自动回滚。

这些能力加入流水线后，需要同时更新本文档、`RELEASING.md` 和对应 workflow。

## README Star 趋势图

README 中的 Star 趋势图由独立的 [`update-star-history.yml`](../.github/workflows/update-star-history.yml) 维护，不进入 Release 产物。工作流每六小时运行一次，也支持手动触发；生成器或工作流发生变更时，会先在 Pull Request 中运行单元测试。

发布任务通过 GitHub stargazers API 获取带时间戳的完整 Star 历史，生成浅色和深色 SVG，并提交到独立的 `star-history` 分支。README 使用该分支的 Raw URL，因此更新图表不会修改 `master`、触发产品发布或污染源码提交历史。任务使用仓库自带的 `GITHUB_TOKEN`，无需额外维护 Secret；只有非 Pull Request 任务拥有写入资产分支的权限。

首次启用必须分两阶段完成，避免 README 在图表发布失败时长期引用不存在的资源：

1. 先合并 workflow、生成器和测试，不加入 README 图片引用；
2. 手动运行 workflow，确认 `star-history` 分支已创建且以下文件可访问；
3. 再单独合并 README 图片引用。

```text
assets/star-history/star-history-light.svg
assets/star-history/star-history-dark.svg
```

## 依赖更新

项目不启用 Dependabot 定期更新。Go Modules、GUI npm、官网 npm 和 GitHub Actions 依赖由维护者按需手动升级；升级前检查上游发布说明，升级后通过 Pull Request 运行路径感知 CI。
