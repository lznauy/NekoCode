// Package languages provides shared tree-sitter language definitions.
package languages

import (
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/rust"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

// Languages maps file extensions to their tree-sitter language objects.
var Languages = map[string]*sitter.Language{
	".go":  golang.GetLanguage(),
	".js":  javascript.GetLanguage(),
	".jsx": javascript.GetLanguage(),
	".mjs": javascript.GetLanguage(),
	".ts":  typescript.GetLanguage(),
	".tsx": typescript.GetLanguage(),
	".py":  python.GetLanguage(),
	".rs":  rust.GetLanguage(),
}

// NewParsers creates a pre-initialized tree-sitter parser for each supported language.
func NewParsers() map[string]*sitter.Parser {
	parsers := make(map[string]*sitter.Parser, len(Languages))
	for ext, lang := range Languages {
		parser := sitter.NewParser()
		parser.SetLanguage(lang)
		parsers[ext] = parser
	}
	return parsers
}
