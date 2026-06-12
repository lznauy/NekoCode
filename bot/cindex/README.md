# CIndex

轻量级代码索引模块，为 NekoCode agent 提供项目结构感知能力。

启动时自动检测项目根目录（查找 `.git`、`go.mod`、`package.json`、`Cargo.toml`、`pyproject.toml` 等标记文件）。在非项目目录（如 `$HOME`）打开时跳过索引，不影响其他功能。

## 功能

- **多语言解析** — 基于 Tree-sitter，支持 Go、TypeScript/JavaScript、Python、Rust
- **符号关系图** — 提取函数、方法、类、结构体、接口、变量、常量，建立调用、包含、导入关系
- **图遍历查询** — BFS/DFS、影响半径、调用链、祖先/后代遍历
- **全文搜索** — SQLite FTS5，按名称、签名、文档搜索
- **增量同步** — fsnotify 文件监听 + 内容哈希，文件变更自动更新索引
- **持久化** — SQLite WAL 模式，索引结果缓存到 `.nekocode/cindex.db`（纯 Go 驱动，无 CGO）

## 原理

### 为什么需要代码索引？

Agent 在处理代码任务时，需要理解项目结构：哪些函数存在、谁调用了谁、包之间的依赖关系。传统方式是用 grep/glob 逐次搜索，但：

1. **慢** — 每次搜索都要遍历文件系统
2. **不完整** — grep 找不到语义关系（调用、继承、导入）
3. **重复** — 每个对话都要重新搜索同样的内容

CIndex 在项目启动时一次性构建**代码知识图谱**，将 O(n) 的文件扫描转化为 O(1) 的内存查找。

### Tree-sitter 解析

[Tree-sitter](https://tree-sitter.github.io/) 是增量解析器，能将源代码解析为具体的语法树（CST）。CIndex 用 tree-sitter query 精确提取符号定义和调用关系：

```
// Go 函数声明
(function_declaration name: (identifier) @name) @func

// Go 方法声明（支持指针接收者）
(method_declaration name: (field_identifier) @name) @method

// Go 变量/常量
(var_spec (identifier) @name) @var
(const_spec (identifier) @name) @const

// Go 方法调用
(call_expression function: (selector_expression field: (field_identifier) @callee)) @call

// Go 导入
(import_spec path: (interpreted_string_literal) @import_path) @import
```

每种语言定义一组 query，分两遍扫描：
- **第一遍** — 提取定义（函数、类、结构体、接口、变量、常量）
- **第二遍** — 提取关系（调用、导入），关联到所属函数

### 图数据模型

所有符号存储为 **Node**，关系存储为 **Edge**，构成一张有向图：

```
Node:HandleRequest ──calls──▶ Node:Format
Node:main.go       ──imports──▶ Node:handler
Node:Greeter       ──contains──▶ Node:Greet
```

图支持双向遍历：
- `GetCallers(id)` — 谁调用了这个函数？（反向）
- `GetCallees(id)` — 这个函数调用了谁？（正向）
- `GetChildren(id)` — 这个结构体包含哪些方法？
- `TraverseBFS/DFS` — 从任意节点出发遍历

### 跨文件引用解析

Parser 产出的边只有函数内调用（同文件）。跨文件引用通过 `resolveReferences` 在第二阶段解析：

1. 构建 `nameIndex`：符号名 → 节点列表
2. 遍历所有 `ToID == 0` 的 call 边，按 `CalleeName` 查找目标节点
3. 同包优先匹配（避免歧义）
4. 更新 `edgesByTo` 索引

Import 边通过目录路径匹配：`import "myproject/handler"` → PkgPath `"handler"`。

### 增量同步

文件变更通过 fsnotify 监听，500ms 防抖后处理：

```
文件变更事件 → 防抖合并 → 删除旧数据 → 重新解析 → 写入图和DB → resolveReferences
```

内容哈希（SHA256）避免重复解析未变更的文件。新目录自动加入 watcher。

## 执行流程

### 启动流程

```
bot.go New()
│
├── initCtxMgr()
│   ├── cindex.LoadProjectContext(cwd)        ← 发现 NEKOCODE.md 文件
│   │   └── 扫描 ~/.nekocode/、项目根目录、.nekocode/rules/
│   │
│   ├── cindex.NewManager(cwd)                ← 创建管理器
│   │   ├── findProjectRoot(cwd)              ← 向上查找 .git/go.mod/package.json 等
│   │   │   ├── 找到 → 以项目根为索引根目录
│   │   │   └── 没找到 → 跳过索引，返回 nil indexer
│   │   ├── os.MkdirAll(.nekocode/)           ← 确保目录存在
│   │   └── NewIndexer(cindex.db)             ← 打开 SQLite
│   │
│   ├── mgr.Init()                            ← 初始化索引
│   │   ├── LoadOrBuild(cwd)                  ← 尝试从 DB 加载
│   │   │   ├── DB 有数据 → LoadGraph()       ← 直接加载
│   │   │   └── DB 为空 → IndexAll(cwd)       ← 全量构建
│   │   │       ├── Clear()                   ← 清空旧数据
│   │   │       ├── buildGraphFromWalk()      ← 遍历+解析+写入
│   │   │       └── resolveReferences()       ← 跨文件引用解析
│   │   │
│   │   └── NewSyncer() + Start()             ← 启动文件监听
│   │       └── fsnotify.Watch(所有目录)
│   │
│   ├── ctxMgr.Add("system", skeleton)        ← 注入项目概览到系统提示
│   └── ctxMgr.Add("system", projCtx)         ← 注入 NEKOCODE.md 内容
│
└── initToolRegistry()
    └── toolRegistry.Register(ProjectInfoTool) ← 注册 project_info tool
```

### 查询流程

```
Agent 调用 project_info tool
│
└── ProjectInfoTool.Execute(query)
    ├── "skeleton"      → graph.FormatSkeleton(cwd)
    │                     输出: <project><language>go</language>...
    │
    ├── "symbol:Foo"    → graph.FindNodesByName("Foo")
    │                     内存查找: 部分匹配，大小写不敏感
    │                     输出: 2 symbol(s) matching 'Foo': func Foo — x.go:1
    │
    ├── "deps:pkg"      → graph.QueryDeps("pkg")
    │                     遍历: pkg 内节点的 edgesByFrom → EdgeImports
    │                     输出: Dependencies of pkg (2): dep1, dep2
    │
    ├── "file:name"     → graph.QueryFile("name")
    │                     遍历: 大小写不敏感的部分匹配
    │                     输出: 2 file(s) matching 'name': handler.go
    │
    └── "search:term"   → db.SearchFTS(term)
                            FTS5 全文搜索: name, signature, doc
                            输出: 3 result(s) for 'term': func Foo — x.go:1
```

### 增量同步流程

```
文件变更 (fsnotify)
│
├── Create 目录 → watcher.Add(新目录)
│
├── Write/Remove 文件
│   ├── 累积到 pendingChanges（防抖 500ms）
│   ├── 定时器触发 → 批量处理
│   │   ├── 文件读取 + 哈希计算（锁外）
│   │   ├── 哈希比对（无变化则跳过）
│   │   ├── 加锁
│   │   │   ├── DB: DeleteFile → SaveNode → SaveEdge → SaveFile
│   │   │   ├── Graph: RemoveFileNodes → AddNode → AddEdge
│   │   │   └── resolveReferences()
│   │   └── 解锁
```

## 架构

```
bot/cindex/
├── manager.go      # 入口管理器，协调各组件，项目根目录探测
├── graph.go        # 核心数据结构（Node, Edge, Graph）+ 查询接口
├── db.go           # SQLite schema、持久化、FTS5 搜索
├── parser.go       # Tree-sitter 解析引擎，提取符号和关系
├── index.go        # 索引编排（全量扫描、跨文件引用解析）
├── sync.go         # 增量同步（fsnotify 监听 + 防抖）
├── traversal.go    # BFS/DFS 图遍历、路径查找
├── tool.go         # project_info tool 接口层
└── project.go      # NEKOCODE.md 项目上下文发现
```

## 使用

```go
// 创建并初始化
mgr, err := cindex.NewManager(cwd)
if err != nil {
    log.Fatal(err)
}
if err := mgr.Init(); err != nil {
    log.Fatal(err)
}
defer mgr.Close()

// 获取图
graph := mgr.Graph()

// 查询符号
symbols := graph.QuerySymbol("HandleRequest")

// 查询依赖
deps := graph.QueryDeps("myproject/handler")

// 全文搜索（需要 FTS5）
nodes, _ := mgr.Indexer().db.SearchFTS("http", 10)

// 格式化项目概览
skeleton := graph.FormatSkeleton(cwd)

// 注册为 tool
tool := cindex.NewProjectInfoTool(mgr)
registry.Register(tool)
```

## Tool 查询语法

`project_info` tool 支持以下查询：

| 查询 | 示例 | 说明 |
|------|------|------|
| `skeleton` | `skeleton` | 项目概览（语言、模块、目录树、依赖图） |
| `symbol:<name>` | `symbol:HandleRequest` | 按名称查找符号（部分匹配，大小写不敏感） |
| `deps:<pkg>` | `deps:myproject/handler` | 查询包的内部依赖 |
| `file:<name>` | `file:handler.go` | 按路径片段查找文件（大小写不敏感） |
| `search:<term>` | `search:http` | 全文搜索（名称、签名、文档，需要 FTS5） |

## 数据模型

### Node（代码符号）

```go
type Node struct {
    ID         int64
    Name       string   // 符号名
    Kind       NodeKind // func, method, type, struct, interface, class, var, const, file
    File       string   // 文件路径
    Line       int      // 起始行
    EndLine    int      // 结束行
    PkgPath    string   // 包路径（目录相对路径）
    Signature  string   // 函数签名
    Doc        string   // 文档注释
    Visibility string   // public, private, protected
}
```

### Edge（关系）

```go
type Edge struct {
    ID         int64
    FromID     int64    // 源节点 ID
    ToID       int64    // 目标节点 ID
    Kind       EdgeKind // calls, contains, imports
    File       string   // 关系所在文件
    Line       int      // 关系所在行
    CalleeName string   // 未解析的被调用函数名
    ImportPath string   // 未解析的导入路径
}
```

### SQLite Schema

```sql
-- 符号表
CREATE TABLE nodes (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    kind TEXT NOT NULL,       -- func, method, struct, interface, class, type, var, const, file
    file TEXT NOT NULL,
    line INTEGER NOT NULL,
    end_line INTEGER,
    pkg_path TEXT,            -- 包路径（目录相对路径）
    signature TEXT,           -- 函数签名
    doc TEXT,                 -- 文档注释
    visibility TEXT           -- public, private, protected
);

-- 关系表
CREATE TABLE edges (
    id INTEGER PRIMARY KEY,
    from_id INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    to_id INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    kind TEXT NOT NULL,       -- calls, contains, imports
    file TEXT,
    line INTEGER,
    callee_name TEXT,         -- 未解析的被调用名
    import_path TEXT          -- 未解析的导入路径
);

-- 文件哈希表（增量同步用）
CREATE TABLE files (
    path TEXT PRIMARY KEY,
    content_hash TEXT NOT NULL,  -- SHA256
    language TEXT,
    indexed_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 全文搜索索引（自动与 nodes 同步）
CREATE VIRTUAL TABLE nodes_fts USING fts5(
    name, signature, doc,
    content=nodes,
    content_rowid=id
);
```

## 依赖

- `github.com/smacker/go-tree-sitter` — Tree-sitter Go binding (CGO)
- `zombiezen.com/go/sqlite` — 纯 Go SQLite 驱动（无 CGO，含 FTS5）
- `github.com/fsnotify/fsnotify` — 文件系统监听

## 测试

```bash
go test ./bot/cindex/ -v
```
