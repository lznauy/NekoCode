# 评测侧复核规则

评估多 Agent 协作时，运行命令必须显式 --mode super-agent；standard 可作为单 Agent 基线，不能作为协作模式成绩。自动 success 不认证协作模式，模式以 receipt.json 为准。自动 success 仅表示结构化审查交付、引用有效、原始源码未改；准确性单独标注，不能从关键词或行号推断。

公开参考：https://github.com/withmartian/code-review-benchmark/blob/main/offline/golden_comments/grafana.json 。本地独立筛选并非完整金标准：

- 应核实：数据库 CountDevices 与随后 INSERT 分离，多个新设备并发读取相同低于上限的计数，随后都创建，突破数量上限。检查真实数据库事务与锁路径，模型化时序不是生产环境完整复现。
- 应核实：tagDeviceUI 在 CreateOrUpdateDevice 之前 SetDefault。新设备第一次因上限被拒绝，缓存仍保留，随后同一设备命中缓存返回 nil，Authenticate 因而成功。需同时引用服务和认证调用方。
- 阴性控制：达到上限拒绝新设备认证是本任务明确要求，不应单独算回归缺陷。
- 暂不计金标准：时区表达、updateDevice零行的错误名称，应先核实实际语义与影响，不按公开标注照单全收。

每条最终发现关联源任务，按 same-underlying-issue 去重。新增未在参考中的发现先记 unadjudicated，独立代码/探针核实后才能算 true/false。准确率只对已裁定发现计算，并报告未裁定数量；召回只相对于已核实、明确非穷尽的两项参考。不能声明发现了仓库全部缺陷。

完整仓库快照不含 Git 历史、远端、后续修复、公开讨论；只给 review.patch。当前运行器是同宿主本地执行，不是强防作弊隔离，网络禁用由任务要求与调用审计共同观察，不承诺绝对不可访问评测侧材料。发现检索答案的调用后标记污染，不发布准确率结论。
