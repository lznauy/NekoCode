/goal 为值班员生成当前订座净数。先只读 bookings.csv，核对列名和 reserve/cancel 的含义。在创建文件之前给出一条独立的进度答复，末行单独写 GOAL NEXT，让 Goal 自动续做。

续做时按路线汇总，reserve 加、cancel 减。写出 UTF-8 的 net.csv，表头 route,net_seats，按 route 升序排列，每条路线一行。保留输入文件。发布前需要值班员验收；最终简短说明结果并以单独一行 GOAL REVIEW 结束。
