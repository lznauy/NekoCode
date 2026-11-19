接手目录恢复工具。生产归档 archive/catalog.json 是旧快照，用 archive/recovery.jsonl 恢复后请交付 archive/recovered.json，保留两份原始归档。修复通用 CLI：python catalog.py SNAPSHOT JOURNAL，成功将快照就地升级/保存为 {"version":2,"records":{"商品编码":{"rev":正整数,"name":字符串或null}}}。v1 字段见归档；已是 v2 也须支持。空日志也要升级。
日志是 UTF-8 JSON Lines（忽略空白行），每条有非空字符串 code、正整数 rev（不接受布尔）、op；put 需要非空字符串 name，delete 产生 name=null 的墓碑。先检查事件有效性，再按每个编码自己的版本处理：旧事件忽略，相同版本同内容是重放，相同版本不同内容是冲突，新版本替换。墓碑可阻止过时的 put 复活，仅更新的 put 可以恢复该商品。
未知快照版本、非法日志或冲突须返回非零，既有快照保持逐字节不变。成功发布须原子替换，使已有读者看到完整文件。仅用 Python 标准库。最终说明如何核验恢复结果和失败路径。

保留原件：archive/catalog.json、archive/recovery.jsonl
