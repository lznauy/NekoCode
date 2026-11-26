# A2A 适配器维护说明

> **主要读者：** 修改 A2A、daemon 或 Runtime 的项目开发者与审查者。部署操作见 [使用者指南](../users/A2A_GUIDE.md)，对外消息契约见 [客户端接入协议](../developers/A2A.md)。

## 修改入口与状态所有权

| 代码 | 职责 |
| --- | --- |
| [`cmd/daemon/a2a.go`](../../cmd/daemon/a2a.go) | 显式启用、监听限制、路由挂载和 daemon 认证包装 |
| [`runtime/a2aapi/server.go`](../../runtime/a2aapi/server.go) | Agent Card、SDK 组装、输入校验和请求交接 |
| [`runtime/a2aapi/executor.go`](../../runtime/a2aapi/executor.go) | Task 到 Run、Context 到 Session 的映射与事件投影 |
| [`runtime/a2aapi/context_retention.go`](../../runtime/a2aapi/context_retention.go) | Context 预留、回滚、轮换和 Session 清理 |
| [`runtime/a2aapi/message.go`](../../runtime/a2aapi/message.go)、[`interaction.go`](../../runtime/a2aapi/interaction.go) | 消息校验、交互回复和 ID 绑定 |
| [`runtime/a2aapi/monitor.go`](../../runtime/a2aapi/monitor.go) | 等待输入后的后台跟踪、Task 更新和失败清理 |
| [`runtime/a2aapi/task_store.go`](../../runtime/a2aapi/task_store.go) | 有界存储、版本检查、淘汰及变更通知 |
| [`runtime/a2aapi/subscription.go`](../../runtime/a2aapi/subscription.go) | 订阅持久化后的 Task 快照 |
| [`runtime/runtime_session_run.go`](../../runtime/runtime_session_run.go) | 原子选择会话、启动运行、恢复原会话和清理保护 |

daemon 默认关闭 A2A，启用后共享现有 Runtime。适配器通过公开 Runtime 能力执行任务和交互；Runtime 不依赖 A2A SDK。普通 `StartRun` 保留已有录制、排队、附件和后台调度路径，Session 专用检查只用于 `StartRunInSession`。共同约束见 [架构边界](ARCHITECTURE_BOUNDARIES.md)。

`Server.Handler()` 的本地身份拦截器只给 Task Store 提供稳定的单租户 owner，认证由宿主提供。自定义宿主必须像 daemon 一样包装受保护的协议 handler，不能把该身份视为客户端已经完成认证。Agent Card 单独公开，不能包含凭据。

## 会话切换与恢复

`StartRunInSession` 持 Runtime mutation 锁，检查前台和后台执行后选择或创建会话，再启动 Run。A2A 不支持 Super Agent，也不允许通过控制命令改变 Context 的 Session 归属。已有 Super Agent Context 的模式恢复命令可以进入命令处理，普通任务仍被拒绝。

Run 的清理在 Runtime 重新空闲之前恢复原会话，然后唤醒后台协调器。恢复服务返回 `MutationCommitted() == true` 时，当前会话已经切换，应保留该选择并报告收尾错误；只有未提交的恢复失败才尝试新会话。恢复和 fallback 都未成功时，Runtime 阻止新 Run，直到显式新建或恢复会话解除失败状态。

标准 Bot 通过可选的 `SessionsServices.CaptureDraft` 提供空会话快照的恢复闭包，不把空会话强行写成磁盘历史。主会话的闭包只保留到本次 Run 清理；A2A 空会话的闭包随 Context 管理，Session 删除时释放，已有持久化历史时使用磁盘恢复。Runtime 不保存 Bot 类型。清理结束后，适配器通过 `WaitRun` 屏障后的 `EventHistory` 读取尾部告警，不能在看到 `RunDone` 时直接忽略恢复结果。

## 前台请求与后台跟踪交接

SDK 在 `INPUT_REQUIRED` 后结束本次执行，但 Runtime 仍可能等待审批或回答。`Cleanup` 为这个间隔启动 monitor，让其他入口解决交互后产生的状态和结果仍能写入 Task Store。

后续发送或取消请求在 SDK 读取 Task 前停止 monitor，并等待其退出。请求级 foreground 预约与 `monitorPauses` 保护交接；失败的重复请求只释放自己的预约，不能替另一个前台执行启动 monitor。流式请求可能多次进入 `After`，释放操作必须幂等。

同一 Run 的并行交互按请求顺序向 A2A 客户端逐项展示。任一入口解决某项交互后，只移除对应 ID；当前项解决后，剩余问题继续保持 `INPUT_REQUIRED`，客户端重连可从 Task 快照读取下一项。交互回复必须核对 `interactionId`，文本回复的 ID 放在消息 metadata，结构化回复可放在 Data Part。无效回复应保留当前提示信息，不得仅凭 Task ID 批准最新 pending 交互。

订阅沿用 SDK 的拦截器边界，通过底层 store 检查 owner。订阅先取得变更通知 channel，再读取快照，覆盖两者之间的更新；持有 entry 引用以便索引淘汰后仍能交付终态。客户端断开只结束其订阅，不取消 Runtime Run。

## 保留与清理

当前保留参数在 `executor.go` 和 `task_store.go` 定义：

| 项目 | 上限与行为 |
| --- | --- |
| Task 数量 | 256；需要空间时淘汰最旧终态 Task，不淘汰活动 Task |
| 单个 Task | 最多 100 条历史消息、16 MiB；先裁剪可丢弃历史，仍超限则失败 |
| Task Store | 总计 64 MiB；写入前检查预算与可回收终态记录 |
| Context | 64；淘汰不活跃且未占用当前 Session 的最旧 Context |
| 单个 Context | 达到 128 轮或累计 1 MiB 输入后，条件允许时轮换 Session |
| Session 删除积压 | 最多 64；后续任务启动前重试，积压达到上限后拒绝创建新 Session |

Task 淘汰与 Context 淘汰分别管理。Context 淘汰或轮换产生的旧专属 Session 在相应 Run 结束后清理；删除经过 Runtime 的 inactive 检查和 mutation 屏障，避免删除其他入口刚选中的 Session。

SDK 持久化 `INPUT_REQUIRED` 失败时，生产者可能已经返回，不能指望它再次读取取消信号。`Cleanup` 必须取消并释放尚未结束的 Run。monitor 写入失败时同样取消 Run，并通过正常版本检查写入最小失败 Task，使其可以淘汰；此时可以舍弃保留产物以满足预算。

## 事件投影与回归检查

文本流需要处理 `assistant_reset`：对同一 Artifact 发出替换更新，撤回废弃内容。撤回后即使没有新的 delta，也要交付已确认消息或最终输出。系统消息使用独立 `system` Artifact，不能遮蔽后续回答。

`result` 最终以确认消息和 `RunResult.Output` 为准。投影通过 builder 记录当前候选内容，内容相同时跳过重复更新；有差异时替换同一个 Artifact。终态处理统一等待清理、交付告警和释放绑定，消费方提前结束迭代后不能再次调用 yield。任务列表分页按更新时间和 ID 的排序键继续查找，不要求游标对应的记录仍存在或保持原更新时间。

修改这些路径时核对以下场景：

- 正常完成、取消、部分会话选择失败、已提交恢复错误和恢复完全失败。
- daemon 其他入口解决审批或问题后的结果、终态与资源释放。
- 重复回复、异步立即返回、前台 / monitor 交接及存储超限。
- 前台和后台文本撤回、没有后续 delta 的最终输出。
- 订阅鉴权、客户端取消、Task 淘汰后终态交付。
- A2A 默认关闭，以及启用后的原 HTTP API、CORS 和后台运行兼容性。

```bash
go test -race ./runtime/... ./cmd/daemon
go test ./...
go vet ./runtime/... ./cmd/daemon
```
