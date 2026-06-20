package index

import graphpkg "nekocode/bot/index/graph"

type NodeKind = graphpkg.NodeKind

const (
	KindFunc      = graphpkg.KindFunc
	KindMethod    = graphpkg.KindMethod
	KindType      = graphpkg.KindType
	KindStruct    = graphpkg.KindStruct
	KindInterface = graphpkg.KindInterface
	KindClass     = graphpkg.KindClass
	KindVar       = graphpkg.KindVar
	KindConst     = graphpkg.KindConst
	KindFile      = graphpkg.KindFile
)

type EdgeKind = graphpkg.EdgeKind

const (
	EdgeCalls    = graphpkg.EdgeCalls
	EdgeContains = graphpkg.EdgeContains
	EdgeImports  = graphpkg.EdgeImports
)

type Node = graphpkg.Node
type Edge = graphpkg.Edge
type FileInfo = graphpkg.FileInfo
type Graph = graphpkg.Graph
type SymbolInfo = graphpkg.SymbolInfo

func NewGraph() *Graph {
	return graphpkg.NewGraph()
}
