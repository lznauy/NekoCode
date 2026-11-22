# Relay Station 业务契约

## 进程接口

从项目根目录执行 `python3 -m relay --db PATH OP`。每次调用只执行一个操作，从标准输入读取一个 UTF-8 JSON 对象，标准输出恰好写一个 JSON 对象。成功退出 0 并返回 `{"ok":true,...}`。业务拒绝退出 2 并返回 `{"ok":false,"error":"错误码"}`。可向标准错误记录诊断。输出允许额外字段。没有后台服务，也没有联网或安装依赖步骤。

PATH 是调用方指定的 SQLite 文件，可含空格和 Unicode。首次操作自动建库。网络、包裹、占用、台账和幂等记录都须随该数据库持久化。打开旧库即可继续运行。两个库相互独立。

必需的标识符、站点、请求键均为非空字符串；站点可用 Unicode。数字字段要求 JSON 整数，布尔值不属于整数。缺字段、非法类型或值、无效 JSON 或未知操作返回 `invalid`。下文的箭头表示成功返回中的业务字段，所有成功包均含 `ok:true`。

## 网络与路径

| OP | 请求 | 成功结果 |
|---|---|---|
| `import` | `{"stations":["A","B"],"links":[{"from":"A","to":"B","minutes":4,"capacity":1}]}` | `ok:true` |
| `route` | `{"origin":"A","destination":"B"}` | `{"path":["A","B"],"minutes":4}` |

导入是增量原子事务。边有方向，时长与并发容量均为正整数，禁止自环。端点须来自当前库或本批站点，否则返回 `unknown_station`。重复导入相同站点和相同定义的边成功；同一有向边的不同定义返回 `conflict`。一项出错，整批撤销。

路径在当前网络上按总分钟数、边数、完整站点标识序列依次升序择优；使用 Unicode 码点顺序。容量只影响发运。已存在的同站查询返回单站路径和 0 分钟；未知端点返回 `unknown_station`，已知但不可达返回 `unreachable`。

## 包裹状态机

| OP | 请求 | 成功结果 |
|---|---|---|
| `create` | `{"id":"P","origin":"A","destination":"C","key":"k1"}` | `{"parcel":投影}` |
| `dispatch` | `{"id":"P","key":"k2"}` | `{"parcel":投影}` |
| `arrive` | `{"id":"P","key":"k3"}` | `{"parcel":投影}` |
| `get` | `{"id":"P"}` | `{"parcel":投影}` |

投影含 `id`、完整 `path`、`state`、`station`。创建时冻结当时的最优路径，起终点必须不同（相同返回 `invalid`）。路径错误沿用 route 错误码；已存在的包裹 ID 返回 `conflict`。初始状态 `ready`，station 为起点。新路线不改变已创建包裹的路径。

dispatch 从 ready 发往冻结路径的下一站，成功后状态为 `in_transit`，station 仍为发站。每件运输中包裹占该有向边一个容量位。已满返回 `capacity`。arrive 从 in_transit 推进一站并释放占位。中间站状态变为 ready，到达终点变为 `delivered`。非法状态转换返回 `state`，未知包裹返回 `missing`。并发争夺最后一个容量位时仅一个成功；业务拒绝及重放均不改变占用。

## 幂等、台账与报告

create、dispatch、arrive 共用以 key 唯一标识的幂等空间。相同 key、操作、业务参数返回第一次成功时的完整响应，跨进程、跨后续状态仍可重放；JSON 对象键顺序没有语义。对已成功 key 改变操作或参数返回 `conflict`。业务失败不消耗 key，后续条件满足时可用原 key 重试。状态、容量、幂等记录和台账必须同事务提交。

| OP | 请求 | 成功结果 |
|---|---|---|
| `ledger` | `{"after":0,"limit":2}`，可选 `id` | `{"events":[{"seq":1,"id":"P","op":"create","key":"k1"}]}` |
| `report` | `{}` | `{"counts":{"ready":0,"in_transit":0,"delivered":0},"busy":[{"from":"A","to":"B","used":1}]}` |

每次成功的新包裹变更记录一条台账。全库从 1 连续编号，仅计算已提交的变更。导入、查询、拒绝和重放均不产生台账。ledger 按 seq 升序，先取 `seq > after` 并匹配可选包裹 ID，再取 limit 项；无结果返回空数组。after 默认为 0，要求非负整数；limit 默认为 100，取值 1 至 100。报告 counts 始终包含三种状态。busy 只列占用大于 0 的边，按 from、to 升序排列。
