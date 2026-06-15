package builtin

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"nekocode/bot/tools"
	"nekocode/bot/tools/hashline"
	"nekocode/bot/treesitter"
)

// GlobalBlockResolver is the tree-sitter based block resolver shared by edit tools.
var GlobalBlockResolver hashline.BlockResolver

// blockNodeTypes are the AST node types considered "blocks" for resolution.
// When the LLM says "replace block N", we find the smallest enclosing node
// of one of these types. Node type names are shared across languages.
var blockNodeTypes = map[string]bool{
	// Functions/methods
	"function_declaration":  true, // Go, JS, TS
	"method_declaration":    true, // Go
	"function_definition":   true, // Python
	"function_item":         true, // Rust
	"function":              true, // JS (arrow/function expression)
	"arrow_function":        true, // JS, TS
	"method_definition":     true, // JS, TS
	// Types/classes
	"type_declaration":      true, // Go
	"class_declaration":     true, // JS, TS
	"class_definition":      true, // Python
	"struct_item":           true, // Rust
	"enum_item":             true, // Rust
	"trait_item":            true, // Rust
	"impl_item":             true, // Rust
	// Composite/struct literals
	"struct_type":           true, // Go
	"interface_type":        true, // Go
	"composite_literal":     true, // Go
	// Import/const/var declarations (block-level)
	"import_declaration":    true, // Go (import block)
	"const_declaration":     true, // Go (const block)
	"var_declaration":       true, // Go (var block)
	"use_declaration":       true, // Rust (use block)
	"const_item":            true, // Rust
	"static_item":           true, // Rust
	// Control flow (shared across languages)
	"if_statement":          true,
	"for_statement":         true,
	"while_statement":       true,
	"switch_statement":      true,
	"select_statement":      true, // Go
	"try_statement":         true,
	"with_statement":        true, // Python
	"export_statement":      true, // JS/TS
	"match_expression":      true,
	// Rust-specific control flow
	"if_expression":         true,
	"for_expression":        true,
	"loop_expression":       true,
}

// InitBlockResolver initializes the global block resolver.
func InitBlockResolver() {
	langParsers := treesitter.NewParsers()

	GlobalBlockResolver = func(path string, line int) (*hashline.BlockSpan, error) {
		ext := filepath.Ext(path)
		parser, ok := langParsers[ext]
		if !ok {
			return nil, fmt.Errorf("block operations not supported for %s files", ext)
		}

		data, err := tools.ReadSafeFile(path)
		if err != nil {
			return nil, err
		}

		tree, err := parser.ParseCtx(context.Background(), nil, data)
		if err != nil {
			return nil, fmt.Errorf("tree-sitter parse error: %v", err)
		}
		if tree == nil {
			return nil, fmt.Errorf("tree-sitter returned nil tree")
		}
		defer tree.Close()

		// Walk the AST to find the smallest block node containing the target line.
		span := findEnclosingBlock(tree.RootNode(), line)
		if span == nil {
			totalLines := strings.Count(string(data), "\n") + 1
			if line < 1 || line > totalLines {
				return nil, fmt.Errorf("line %d out of range (file has %d lines)", line, totalLines)
			}
			return nil, fmt.Errorf("line %d is not inside a function, method, or code block", line)
		}
		return span, nil
	}
}

// findEnclosingBlock walks the AST bottom-up to find the smallest block
// node that contains the given 1-based line number.
func findEnclosingBlock(root *sitter.Node, targetLine int) *hashline.BlockSpan {
	// Use a recursive search: find the deepest node of a block type
	// whose range contains targetLine.
	var best *hashline.BlockSpan
	var commentAnchored *sitter.Node // nearest comment whose next sibling is a block

	var walk func(node *sitter.Node)
	walk = func(node *sitter.Node) {
		startLine := int(node.StartPoint().Row) + 1
		endLine := int(node.EndPoint().Row) + 1

		// Check if this node contains the target line.
		if targetLine < startLine || targetLine > endLine {
			return
		}

		// If the target line lands on a comment, remember this comment node
		// in case no parent block is found — its next sibling may be a block.
		typeName := node.Type()
		if typeName == "comment" {
			if commentAnchored == nil ||
				(endLine-startLine) < (int(commentAnchored.EndPoint().Row)-int(commentAnchored.StartPoint().Row)) {
				commentAnchored = node
			}
		}

		// If this is a block-type node, check if it's smaller than current best.
		if blockNodeTypes[typeName] {
			if best == nil || (endLine-startLine) < (best.End-best.Start) {
				best = &hashline.BlockSpan{Start: startLine, End: endLine}
			}
		}

		// Recurse into children.
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			walk(child)
		}
	}

	walk(root)

	// Fallback: target line is a comment; try the next sibling.
	if best == nil && commentAnchored != nil {
		if next := commentAnchored.NextNamedSibling(); next != nil {
			if blockNodeTypes[next.Type()] {
				sLine := int(next.StartPoint().Row) + 1
				eLine := int(next.EndPoint().Row) + 1
				best = &hashline.BlockSpan{Start: sLine, End: eLine}
			}
		}
	}

	return best
}
