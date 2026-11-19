核对 defaults.json 和 tenant.json 中 north 租户的生效并发数。未配置字段继承默认值。只有生效值不是 6 时才调整 north，其他租户保持原样，否则保留原文件。最后仅回复 JSON，字段 effective 为最终整数，changed 为是否发生修改。
