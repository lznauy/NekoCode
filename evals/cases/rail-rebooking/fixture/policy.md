截止分钟为 200，含端点。requests 先排除晚到记录，再按 ticket 取最大 revision；相同记录去重。最高版本存在不同内容即 conflict，不得回退到旧版。否则按 state 判定 cancel 或 live。保留所有出现过的票号（本批均有截止前版本）。
容量从 capacity 起算。adjustments 中只应用截止内的事件，同一 event 只应用一次。路径按 sequence 排列，一个团必须在同一路径每一段都有足额座位。live 请求按 tier A、B、C，再按票号升序处理。先试 first，再试 second；两者都失败就 wait。失败的尝试不得占座，成功只在选中路径逐段扣减 party。
