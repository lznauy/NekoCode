// Package treesitter provides shared tree-sitter language definitions
// used by the code indexer (index/parser).
package treesitter

import (
	"nekocode/bot/index/treesitter/languages"
)

// Languages maps file extensions to their tree-sitter language objects.
var Languages = languages.Languages
