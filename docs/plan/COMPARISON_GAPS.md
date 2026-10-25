# 对比补齐方向

> **主要读者：** NekoCode 项目开发者与维护者。记录相对主流产品的短板和补齐顺序，不承诺实现时间。
>
> 更新日期：2026-10-25。对照基准：Claude Code、Codex CLI、Pi、CodeBuddy Code 的公开文档与仓库（能力以各自最新版本为准）。
> 本文只写“还差什么、怎么补、怎么验收”。补齐后修改对应现状文档，再从这里移除。
> 现状基线见 [整体架构](../maintainers/ARCHITECTURE.md)、[用户使用指南](../users/USER_GUIDE.md)、[Hook 指南](../developers/HOOK_GUIDE.md)、[自定义 Agent](../developers/AGENT_GUIDE.md)。

## 一、判断标准

只收三类条目：

1. **能力缺口**：对手有、NekoCode 没有或有明显表达力差距，且影响日常使用。
2. **工程差异化未完成**：NekoCode 方向正确，但只做了一半，用户拿不到完整收益。
3. **生态短板**：短期补不齐，先定“兼容优先，不重复造轮子”的边界。

不收“功能数量对齐”，也不把对手的实验性能力当验收目标。

## 二、优先级与验收

| 优先级 | 条目 | 现状基础 | 还差什么 | 验收方式 |
| --- | --- | --- | --- | --- |
| P0 | `acceptEdits` 权限模式 | `permission.ModeManual/ModeAuto/ModeFull`（`bot/extension/tool/runtime/permission/mode.go`） | 允许文件编辑自动通过、Shell 仍按规则审批；与 Plan Mode、deny 规则的优先级写清楚 | 新增模式测试：编辑放行、`deny` 仍拦截、Shell 仍需审批；用户指南补一节 |
| P0 | 索引维护入口 | `index` 工具与 `.nekocode/index.db`（`bot/extension/tool/builtin/index/core/README.md`） | `/index status`（语言、文件数、最近同步）与 `/index rebuild`；索引不可用或损坏时的提示 | 损坏索引后重建成功；状态输出与索引库一致；故障排查补条目 |
| P1 | Agent 增加 `model` 与 `permissionMode` | Profile 已有 `name`、`description`、`base`、`tools`、`skills`、`max_steps`（`docs/developers/AGENT_GUIDE.md`） | 子 Agent 独立模型与审批模式；与 `compactionModel` 等内部客户端的关系 | 同一任务用不同 profile 跑通；越权工具仍被拒；Agent 指南补字段表 |
| P1 | 网络白名单与出口代理 | 沙箱只有“授权网络 = 共享宿主网络 / 不授权 = 断网”（`bot/extension/tool/runtime/sandbox/impl/native.go`） | `net.outbound` 从布尔扩展为域集合；出口代理与域名确认；审批卡展示目标域名 | 未授权域被拒并给出可诊断错误；授权域可访问；沙箱与权限文档同步 |
| P2 | 中间层 Hook 事件 | 现有 14 个事件（`bot/policy/contract/types.go`） | `PostToolUseFailure`、`FileChanged`、`CwdChanged`、`PermissionRequest`/`PermissionDenied` | 每个事件有触发测试；Plugin 协议版本与兼容说明更新 |
| P2 | 子 Agent worktree 隔离 | 上下文隔离已有，文件隔离没有 | 隔离策略、并发写同一仓库的冲突处理、清理与失败恢复 | 两个 coder 改同一文件不互相覆盖；异常退出后的 worktree 可回收 |

## 三、明确不做的事

- 自建插件市场、云会话协同：插件兼容 `.claude-plugin/plugin.json`，IM 渠道已覆盖；只维护“可直接运行的 Claude Code 风格插件”兼容清单，用 3 个不同形态插件验证安装与运行。
- 不为对齐事件数量而堆 Hook 事件；只补能表达“失败、变更、审批”语义的缺口。
- 不追 Claude Code 的实验性 Agent teams、云端 Cowork 或 Remote Control；NekoCode 的替代路线是 IM 渠道与本地会话恢复。
- 不在 Pi 的“无内置权限系统”方向上做取舍：NekoCode 继续保留内置沙箱与审批，这是既有差异。
- 不引入 USD 预算、供应商费用模拟等无法从协议真实获得的数据。

## 四、推进方式

每个条目独立提交：先在本文件补“设计要点”，再改代码与测试，最后修正现状文档并从本表移除。跨条目共享的改动（如权限模式与子 Agent `permissionMode`）先做前者，后者复用。
