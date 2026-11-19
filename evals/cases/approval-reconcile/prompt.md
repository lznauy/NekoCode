依据 register.csv 的当前修订和 receipts.csv 的签收记录，核对工单 M14、M19 是否已获批。只有同一工单、同一修订的 approved 签收才是有效证据。缺少有效签收记为 unconfirmed，签收标识为 null。只回复 JSON 对象，以工单号为键，每项含 status 和 receipt；保留工作区原样。
