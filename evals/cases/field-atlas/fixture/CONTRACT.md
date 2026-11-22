# Field Atlas 离线手册发布契约 v1

Python 3 标准库，项目根目录为当前工作目录。所有调用都是独立进程。公开命令如下。

- `python3 -m atlas plan SOURCE DEST` 计算拟发布差异，不创建或更改 DEST。
- `python3 -m atlas build SOURCE DEST` 发布并返回与同状态 plan 相同的差异。
- `python3 -m atlas pack DEST FILE.zip` 从已发布目录导出离线包。

成功退出 0，stdout 单个 JSON 对象。plan/build 返回 `{"written":[...],"removed":[...]}`，路径相对 DEST，以 "/" 分隔，两个列表均按字符串升序排列。允许额外字段。pack 返回 `{"entries":N}`。业务错误退出 2，stdout 为 `{"error":"错误码"}`，不改变上次成功发布的任何文件。输入与输出路径可以包含空格及 Unicode。

## 输入和编译

SOURCE/site.json 为 `{"default":"en","locales":["en","fr"]}`。locales 非空不重复，包含 default。SOURCE/pages/*.json 每个文件是一个页面，文件名不定义身份，内容为 `{"id":"guide","variants":{"en":{"title":"Guide","blocks":[{"text":"hello"},{"include":"intro"},{"link":"help"}]}}}`。每页必须有默认语言，变体只允许使用已声明的语言。id 与 locale 均须匹配 `[a-z][a-z0-9-]*`。重复页面 id、错误类型、非法标识符、畸形 JSON、未知块类型、一个块携带多个字段均返回 invalid。title 与 text 是字符串；包括空字符串的原始文本须原样保留。校验所有语言及页面。

为每个 (locale,id) 生成 `pages/LOCALE/ID.json`，对象恰含 `id,locale,title,lines`。对每次查找，优先请求的语言，缺失时使用默认语言；递归 include 和 link 继续使用最初请求的语言。text 往 lines 追加原文；include 原地展开目标正文（不插入标题，重复引用重复展开）；link 追加 `[目标标题](目标id.json)`。未知引用返回 missing；include 有向环返回 cycle。互相 link 合法。另生成 `index.json`，一个按 (locale,id) 字符串升序排列的数组，每项恰含 `id,locale,title,path`；path 为相对输出目录的页面路径。JSON 对空白和转义没有约定，但相同内容多次产出的字节必须确定。

## 增量和交付

输出目录由工具管理，包含且只包含 index.json 和生成页面。plan 和 build 需比较最终产物字节；相等的文件不计 written 且保留修改时间。源文件改名、调整无语义的键顺序、不同机器目录前缀不会影响生成结果。删除源页面后清理其所有语言版本。再次执行 build 或从空目录完整构建，生成物必须逐字节相同。损坏的已发布文件在下一次 build 中得到修复。任何输入错误均先于发布写操作返回。

pack 归档当前发布文件；条目为有序的相对 POSIX 路径、无目录条目，解压后的文件与 DEST 字节一致。相同内容生成的 ZIP 须逐字节一致，即使源文件的 mtime 变化或输出 zip 路径不同。
