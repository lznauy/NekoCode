# 对比补齐方向

> **主要读者：** NekoCode 项目开发者与维护者。记录相对主流产品的短板和补齐顺序，不承诺实现时间。
>
> 更新日期：2026-10-27。对照基准：Claude Code、Codex CLI、Pi、CodeBuddy Code 的公开文档与仓库（能力以各自最新版本为准）。
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
| P2 | 中间层 Hook 事件 | 16 个内部触发点、17 个配置事件名；已支持工具失败过滤及 `PermissionRequest`/`PermissionDenied` 审批观察 | 按实际消费需求补 `FileChanged`、`CwdChanged`，先明确触发边界与事件数据 | 每个新增事件有触发与不触发测试；更新 Hook 指南及协议兼容说明，按契约变化判断是否升级版本 |

## 三、按需评估，不列入当前补齐队列

### `acceptEdits` 权限模式

当前内置规则已默认允许 `write`、`edit`，文件访问仍受工作区边界限制；Shell 按权限规则放行或审批，Plan Mode 和 deny 规则继续约束执行。基础见 `bot/extension/tool/runtime/permission/builtin.go` 与 `bot/extension/tool/runtime/runner/executor_execute_one.go`。因此，“工作区内编辑免审批、Shell 按规则审批”已与现有行为重叠，不再作为 P0 新模式推进。

尚需澄清的是：是否存在现有模式与规则无法表达的编辑审批需求。先核实现有文档和回归测试是否充分覆盖编辑放行、显式 ask/deny、Plan Mode 与工作区外访问；有遗漏则补对应说明或测试。只有明确了新增模式的独有行为和使用场景，才重新评估实现，不将工作区外写入自动纳入授权。

### 子 Agent 独立 `permissionMode`

当前 Profile 工具白名单限定 Agent 能调用的工具，子 Agent 实时继承父级审批模式和权限规则；父级切回 manual 也会收回子 Agent 的 auto/full。实现见 `bot/extension/tool/runtime/runner/executor.go` 的 `ConfigureChildPermissions`。目前优先使用这两层控制，不新增配置字段；`permissionMode` 仍是不支持的字段。

重新启动此项的条件是出现明确需求，例如“父 Agent 自动审批，但某个有写入或 Shell 工具的子 Agent 仍需逐次确认”。届时先定义子 Agent 只能收紧父级授权的边界，再验证父级撤权即时生效、工具白名单和 deny 不可绕过，并补 Agent 指南。

### 网络出口扩展

候选范围为非 Linux 域名隔离、上游企业代理链以及 SSH/UDP 等协议。出现明确使用需求后，先定义目标平台、协议边界与验收方式，再独立评估，不列入当前补齐队列。现有执行边界见 [Shell 网络域名限制](../developers/NETWORK_EGRESS.md)。

## 四、明确不做的事

- 自建插件市场、云会话协同：插件兼容 `.claude-plugin/plugin.json`，IM 渠道已覆盖；只维护“可直接运行的 Claude Code 风格插件”兼容清单，用 3 个不同形态插件验证安装与运行。
- 不为对齐事件数量而堆 Hook 事件；失败过滤已支持，变更与审批事件按实际需求补齐。
- 不追 Claude Code 的实验性 Agent teams、云端 Cowork 或 Remote Control；NekoCode 的替代路线是 IM 渠道与本地会话恢复。
- 不在 Pi 的“无内置权限系统”方向上做取舍：NekoCode 继续保留内置沙箱与审批，这是既有差异。
- 不引入 USD 预算、供应商费用模拟等无法从协议真实获得的数据。

## 五、推进方式

每个条目独立提交：先在本文件补“设计要点”，再改代码与测试，最后修正现状文档并从本表移除。存在共享依赖时，先完成基础能力，后续条目复用；按需评估项满足启动条件后，再进入补齐队列。
