完成铁路中断后的团体改签结算。按 policy.md 将四个 CSV 合并为截止快照，先确定每张票的有效状态，再分配容量，交付以下四份 UTF-8 CSV：
- snapshot.csv：ticket,revision,state，state 为 live、cancel 或 conflict。
- decisions.csv：ticket,result,path，result 为 first、second、wait、cancel 或 conflict；仅成功分配时填写 path。
- capacity-final.csv：leg,available,used,remaining，available 是调整后的初始容量，used 是实际占座数。
- reconciliation.csv：result,tickets,passengers，固定按 first、second、wait、cancel、conflict 排列，含零组。passengers 为相应组的最终团体人数合计，cancel 和 conflict 两组统一填 0。
前三表分别按 ticket、ticket、leg 升序。原始材料保持原样，不留下其他文件。
