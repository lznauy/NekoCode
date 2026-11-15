# 通用 Agent Harness 边界

核心负责执行身份、权限、上下文连续性、依赖/并发、取消、资源、证据传递与交付状态。任务内容和质量标准由委托、Skill、Profile 指令或外部验收器定义。开发、调研、设计、审查均使用同一执行协议；核心不理解缺陷严重性、游戏评分或研究结论是否正确。

## 执行与交接

`task_submit` 接受独立工作；`resume_id` 在原任务、上下文和工作区续做，恢复不会扩展工具权限，也不要求重复声明 `required_tools`。`max_steps` 是单次 attempt 的普通模型轮次上限，后台默认 12，同步子代理沿用 profile 默认；恢复时重新指定本次额度。达到额度在工具批次边界交回控制，既不打断进行中的修改，也不自动重放取消的工作。相同源代码背景优先由一个任务取证，再通过明确的 `task_result` 引用共享；独立复核聚焦可推翻具体主张的检查。全树调用 admission、交接与协调预留独立于任务语义。

新 worker 完成工具必须显式给出 `outcome`，它是调度声明：

| 状态 | 生命周期 | 调度 |
|---|---|---|
| complete | 委托范围 completed，unfinished 必须为空 | 独立 batch 可合并交付 |
| continue | partial，保留 unfinished 与 next_action | 独立 batch 可合并交付，后续由协调者决定是否 resume |
| blocked | partial，保留前置条件缺口 | 及时唤醒协调 |
| decision | partial，保留需要决策的问题 | 及时唤醒协调 |

Runtime 在最终结算处执行非 complete→partial 的约束，直接使用公共 BackgroundHost 的执行器也受约束。宿主错误、取消状态优先；partial 不能满足 `after` 的成功依赖。审批、用户问题和用户新输入不因批处理被压住。生命周期状态不证明测试通过或成果正确。

持久化变更串行提交，状态锁只保护内存；快照、结果文件和恢复读取在状态锁外执行。准入、依赖变化和追加更新先保存候选记录，再发布内存状态。运行中的取消信号不等待存储锁；worker 返回后，在状态锁内确定本次 attempt 的结算归属，先于该边界的取消生效，进入结算后的取消不改写该 attempt 的结果。停止全部任务先发出取消，再串行保存停止回执。

新建任务默认 batch，显式 immediate 用于逐阶段决策。省略 outcome 的旧执行器保持原有保守策略；resume 省略 review_policy 保留保存策略，不强制迁移旧任务。

## 证据与质量

交接携带简短 summary、evidence、files、实际 verification/checks、unfinished、verification_gaps 和 risks；unfinished 只记录委托范围内仍需执行的工作，环境限制、未验证假设及范围外检查放入 verification_gaps，不因此阻止范围已完成的任务交接。完整日志留在已有产物。结构化预览超限时仍保留调度元数据，正文通过 canonical 报告或精确绑定的分页引用读取。稳定引用、短 cursor、保存失败回滚与作用域校验是核心机制。

Runtime 在执行器启动前分配并保存本次 attempt 身份，再通过执行上下文交给执行器；标准子代理用同一身份创建 child attempt。worker 事件不能替换该身份。重启先核验同身份的正式记录：已提交则重建执行结算与通知投影，未提交则保持 interrupted，均不重放 worker。损坏或身份不匹配的正式记录明确报错；缺少 attempt 身份的旧回执保持保守恢复，不猜测其他 attempt 的报告。

公共 `BackgroundWork.Run` 一次返回 `BackgroundResult`，完整 `Review` 与结果同时结算，替代独立的 review 回调。Runtime 在 session 的 `background-results/` 中按 task/attempt 保存不可变记录，`background.json` 只保存引用和有界预览；`task_result` 从该记录派生分页内容。单条记录最多 8 MiB，超过上限或保存失败明确结算为 failed，不静默裁剪为完整报告。通知大小限制独立于存储大小限制。完整记录已提交后，通知回执保存失败只标记 delivery 失败并允许重试，不改写已保存的执行结算。旧 child-session 报告兼容读取；新后台 attempt 在模型执行前持久化 runtime 结果归属，保存错误和在途状态不能触发旧存储回退，调度缓存淘汰后也不例外。删除任务同时删除其结果记录；调度历史淘汰不会撤销已发出的证据引用。

任务特有格式（审查 findings、调研来源对照、游戏功能清单等）写入委托或 Skill，质量由对应验收器判定。核心没有默认 `task_findings` 工具、finding schema、逐候选裁决或审查专用收尾提醒。早期清单实验已停用，生产协议不再维护该未发布实验的兼容类型。

## 验证范围

回归覆盖三类任务共用的交接协议、错误/旧协议优先级、完整报告读取、普通 partial 合并交付、公共 Host 非完成交接不放行依赖，以及旧格式/超长报告不丢失未完成状态。另覆盖正式记录已提交但回执未保存的重启窗口、准入保存失败不启动执行器、损坏或旧 attempt 记录不能恢复成成功。三类协议用例是确定性契约测试，不代表实际模型跨任务能力已经提高。

这轮没有引入新的模型评判器、自动语义去重、自动续做或通用最终提交工具。最终准确性、协调 token 与整体交付效果仍需使用多个 case 做版本对照，不能从单个审查题推导总体提升。
