// Package treesitter provides shared tree-sitter language definitions
// used by the code indexer (index/parser).
package treesitter

import (
	sitter "github.com/smacker/go-tree-sitter"

	"nekocode/bot/index/treesitter/languages"
)

// Languages maps file extensions to their tree-sitter language objects.
var Languages = languages.Languages

// NewParsers creates a pre-initialized tree-sitter parser for each supported language.
func NewParsers() map[string]*sitter.Parser {
	return languages.NewParsers()
}
