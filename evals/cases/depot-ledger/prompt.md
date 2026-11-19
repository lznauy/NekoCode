把现有单文件库存查看器交付为可在本地运行的持久化多仓预留服务 Depot Ledger，Python 标准库即可。兼容原有命令 python depot.py DB：对已有库输出按 site,sku 排序的 site<TAB>sku<TAB>实物数量，每行一条。新增 python depot.py DB api，每进程从 stdin 读一个 JSON 对象，向 stdout 写一个 JSON 对象后退出。所有时间由请求提供，无后台时钟。交付实现、可执行的回归测试（python -m unittest discover）和 README.md（初始化/升级、请求示例、故障恢复与测试方法）。

持久化与迁移：init 在不存在的 DB 创建空 v2；对 legacy.sql 所示的 v1 SQLite 库原地升级至 PRAGMA user_version=2，保留全部余额。重复 init 安全；更高版本拒绝，保持原数据和版本。其余请求均在 init 后发出。每个成功状态变更在进程退出后持久。多个进程竞争时应与某一串行执行等价。

协议：请求以下面字段命名。成功响应可含其他字段；错误须返回 {"ok":false,"error":"错误码"}（可附诊断），业务错误应正常结束进程。
- {"op":"init"} -> {"ok":true}。
- {"op":"receive","key":"…","site":"…","sku":"…","qty":3} 增加实物数。
- {"op":"reserve","key":"…","id":"…","expires":20,"lines":[{"site":"…","sku":"…","qty":2},…]}。每个 id 只可创建一次。先按 site、sku 合并重复行，再以可用量检查所有行，整单占用或整单拒绝。非空标识，数量和 expires 为正整数，布尔值无效，lines 非空。错误码 invalid、shortage 或已使用 id 的 conflict。
- {"op":"ship","key":"…","id":"…"} / {"op":"cancel","key":"…","id":"…"} 仅作用于 active 单。发货扣除实物并解除占用；取消只解除占用。id 不存在返回 missing；非 active 返回 state。
- {"op":"sweep","key":"…","now":20} 将所有 expires<=now 的 active 单变成 expired，释放占用。now 是非负整数。响应含 count，即本次过期单数。仅 sweep 触发过期。
- {"op":"stock"} -> {"ok":true,"rows":[{"site":"…","sku":"…","on_hand":3,"held":1,"available":2},…]}。返回所有已建库存项，按 site,sku 排序，库存为零仍保留。
- {"op":"audit","after":0,"limit":10} -> {"ok":true,"events":[{"seq":1,"op":"…","key":"…"},…]}。序号从 1 连续增长，按 seq 升序，只返回 seq>after 的前 limit 条。每次成功 receive/reserve/ship/cancel/sweep 记录一个事件，零条过期的 sweep 也记录；init、读取和失败不记录。after 为非负整数，limit 为 1..100。

一致性：每个带 key 的成功请求连同响应、审计和业务数据原子落库。相同 key 和相同 JSON 内容（对象字段顺序无关）跨进程重试返回首次完整响应，即使业务状态已经变化；同 key 不同内容返回 conflict。失败不得消耗 key、留下部分写入或审计。未知 op、无效字段值和畸形 JSON 返回 invalid；未来数据库版本的 init 返回 version。请通过公开命令验证最终成品。
