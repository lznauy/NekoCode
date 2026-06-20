// Package treesitter provides shared tree-sitter language definitions
// used by both the code indexer (index) and the block resolver (builtin).
package treesitter

import (
	sitter "github.com/smacker/go-tree-sitter"

	"nekocode/bot/treesitter/languages"
)

// Languages maps file extensions to their tree-sitter language objects.
var Languages = languages.Languages

// NewParsers creates a pre-initialized tree-sitter parser for each supported language.
func NewParsers() map[string]*sitter.Parser {
	return languages.NewParsers()
}
