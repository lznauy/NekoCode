请先运行 python3 -m unittest discover 获取当前失败，再读取相关代码核查根因。只诊断，不修改文件。最终仅返回 JSON，字段 test 为失败方法名，actual_key 和 expected_key 分别为实现当前使用和应使用的字段名。
