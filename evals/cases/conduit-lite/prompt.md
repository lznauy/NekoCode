把此仓库交付成能在本机演示的 Conduit Lite 文章浏览产品，使用 Python 标准库，不引入第三方依赖。
公开参考：RealWorld API spec（spec_excerpt.md）。这是缩小版 demo：固定 alice-demo/bob-demo 身份、内存数据、文章列表 GET /api/articles、更新正文 PUT /api/articles/{slug}，无需注册登录或完整 RealWorld 功能。
阅读现有 app.py，核实并修复实际成立的边界 bug；不要只修表面返回值。列表应先组合 tag/author 精确筛选，再按 createdAt 降序分页；articlesCount 是匹配总数，筛选不裁剪文章的其他标签，查询不能修改原始数据。默认 limit=20/offset=0，limit 合法范围1..100，offset>=0，非整数或越界返回422。非作者PUT返回403且不能改变文章，缺少有效demo身份返回401。
完成 index.html：可用的文章列表、tag/author 筛选、每页2条和前后翻页，清楚显示总数和错误，切换筛选重置页码；通过DOM文本方式显示用户内容。写运行说明、适当回归测试，并实际运行验证。
最终说明实现结果、启动方法和实际验证结果。自主选择工作方式，不需要为了交付做无关增强。
