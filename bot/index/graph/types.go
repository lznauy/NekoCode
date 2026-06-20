package graph

// SymbolInfo is a compatibility type for project_info symbol queries.
type SymbolInfo struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	PkgPath string `json:"pkg_path"`
}
