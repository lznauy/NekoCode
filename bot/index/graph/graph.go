package graph

// NodeKind represents the type of a code symbol.
type NodeKind string

const (
	KindFunc      NodeKind = "func"
	KindMethod    NodeKind = "method"
	KindType      NodeKind = "type"
	KindStruct    NodeKind = "struct"
	KindInterface NodeKind = "interface"
	KindClass     NodeKind = "class"
	KindVar       NodeKind = "var"
	KindConst     NodeKind = "const"
	KindFile      NodeKind = "file"
)

// EdgeKind represents the type of a relationship between nodes.
type EdgeKind string

const (
	EdgeCalls    EdgeKind = "calls"
	EdgeContains EdgeKind = "contains"
	EdgeImports  EdgeKind = "imports"
)

// Node represents a code symbol (function, class, variable, etc.).
type Node struct {
	ID         int64    `json:"id"`
	Name       string   `json:"name"`
	Kind       NodeKind `json:"kind"`
	File       string   `json:"file"`
	Line       int      `json:"line"`
	EndLine    int      `json:"end_line"`
	PkgPath    string   `json:"pkg_path"`
	Signature  string   `json:"signature,omitempty"`
	Doc        string   `json:"doc,omitempty"`
	Visibility string   `json:"visibility"` // public, private, protected
}

// Edge represents a relationship between two nodes.
type Edge struct {
	ID         int64    `json:"id"`
	FromID     int64    `json:"from_id"`
	ToID       int64    `json:"to_id"`
	Kind       EdgeKind `json:"kind"`
	File       string   `json:"file,omitempty"`
	Line       int      `json:"line,omitempty"`
	CalleeName string   `json:"callee_name,omitempty"` // unresolved callee name for call edges
	ImportPath string   `json:"import_path,omitempty"` // unresolved import path for import edges
}

// FileInfo tracks indexed files and their content hashes.
type FileInfo struct {
	Path        string `json:"path"`
	ContentHash string `json:"content_hash"`
	Language    string `json:"language"`
}

// Graph is the in-memory code knowledge graph.
type Graph struct {
	Nodes map[int64]*Node
	Edges map[int64]*Edge

	// Indexes for fast lookup
	nodesByName map[string][]*Node   // name → nodes
	nodesByFile map[string][]*Node   // file → nodes
	edgesByFrom map[int64][]*Edge    // fromID → edges
	edgesByTo   map[int64][]*Edge    // toID → edges
	files       map[string]*FileInfo // path → file info

	nextNodeID int64
	nextEdgeID int64
}

// NewGraph creates an empty graph.
func NewGraph() *Graph {
	return &Graph{
		Nodes:       make(map[int64]*Node),
		Edges:       make(map[int64]*Edge),
		nodesByName: make(map[string][]*Node),
		nodesByFile: make(map[string][]*Node),
		edgesByFrom: make(map[int64][]*Edge),
		edgesByTo:   make(map[int64][]*Edge),
		files:       make(map[string]*FileInfo),
	}
}
