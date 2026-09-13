# 贡献指南

欢迎贡献！NekoCode 的定位是**单二进制、可嵌入、缓存成本敏感的 AI 编程助手核心**。提交前请先读本指南和 [ARCHITECTURE.md](ARCHITECTURE.md)。不要在未讨论的情况下提前实现尚未确定的功能。

## 开发环境

- Go 1.25.12（版本以 `go.mod` 为准）
- 可选：Zig（交叉编译 CGO 依赖时使用，见 CI 的 Zig CC wrapper）
- 可选：Node.js 20+（GUI 前端和 `official` 官网需要）

常用命令：

```bash
go build ./...                      # 编译全部
go test ./...                       # 全量测试
go vet ./...                        # 静态检查
go test -race ./bot/... ./runtime/...  # 竞态检测（并发核心，CI 必跑）
govulncheck ./...                   # 检查 Go 代码可达的已知漏洞
```

## 代码规范

- 遵循仓库既有分层：`bot`（核心）→ `runtime`（适配）→ `interaction`（界面/连接器）；不要跨层绕行。
- 保持**最小充分改动**：不添加未请求的功能、配置、抽象；也不顺手重构、重命名或修改无关注释。
- 错误处理保留真实失败语义：不吞错、不伪造成功、不加无操作回退。
- 注释解释非显而易见的意图、约束和取舍，不复述代码。
- 新增代码需通过 `gofmt`，并保持 `go vet` 干净。

## 测试要求

- 新增行为或修复 bug 必须带测试（与风险相称的聚焦测试）。
- 测试保护**可观察行为、边界和不变量**，不测实现文本或偶然默认值。
- 涉及缓存或前缀稳定性的改动，注意项目有字节级 golden 测试（`bot/prompt`）。误改 system prompt 会使 CI 失败。
- 并发、异步、生命周期改动，CI 的 `-race` 会兜底数据竞争，本地也建议先跑。

## 提交与 PR

- **一个任务一个提交、可独立回滚**。
- 提交信息简述改动与原因，并引用相关 issue。
- 用户可见行为变化必须更新 `docs/CHANGELOG.md` 对应分节。
- PR 按 `.github/PULL_REQUEST_TEMPLATE.md` 填写 Summary 与 Test Plan，确保：
  - [ ] Build passes（`go build ./...`）
  - [ ] Tests pass（`go test ./...`）
  - [ ] `docs/CHANGELOG.md` updated（如有用户可见变化）
  - [ ] 手动验证关键路径
- 安全敏感改动需要在 PR 中说明威胁模型、信任边界和失败模式。

## CI 约定

合并前必须通过 `.github/workflows/go.yml` 的全部步骤：

1. `gofmt` 和 `go build ./...`
2. `go test ./...`
3. `go vet`
4. `go test -race`（并发核心）
5. `govulncheck`（依赖漏洞扫描）
6. GUI 的 Vitest 与 TypeScript/Vite 构建
7. 官网的 ESLint 与 Next.js 构建
8. ShellCheck 和文档链接检查

## 文档

行为、架构或用户界面变化时，同步更新 `docs/`：

- `ARCHITECTURE.md`：核心模块职责与数据流
- `USER_GUIDE.md`：用户可见功能

## 安全

- 发现安全漏洞不要开公开 issue，请按 [SECURITY.md](SECURITY.md) 中的私有渠道报告。
- 涉及权限、命令执行、文件写、凭据、隐私日志的改动需要格外谨慎，先说明威胁模型。
- 依赖新增时确认维护状态；CI 的 govulncheck 是底线，不是全部。

## 前端（GUI）

`interaction/gui/web` 目前仍在开发期，未随 Release 发布：

- 前端改动请本地跑 `npm test`（vitest）与 `npm run build`（tsc 类型检查）。
- CI 会运行 Vitest 和 TypeScript/Vite 构建。Wails 桌面打包仍需按关键路径手动验证。

## 社区规范

参与讨论和贡献代码即表示同意遵守 [行为准则](CODE_OF_CONDUCT.md)。维护和发布职责见
[GOVERNANCE.md](GOVERNANCE.md)，支持范围见 [SUPPORT.md](SUPPORT.md)。
