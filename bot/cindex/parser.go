package cindex

import (
	"context"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/rust"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

// langConfig holds tree-sitter language and query patterns.
type langConfig struct {
	lang    *sitter.Language
	queries map[string]string // query name → pattern
}

var supportedLangs = map[string]langConfig{
	".go": {
		lang: golang.GetLanguage(),
		queries: map[string]string{
			"functions": `(function_declaration name: (identifier) @name) @func`,
			"methods":   `(method_declaration name: (field_identifier) @name) @method`,
			"types":     `(type_declaration (type_spec name: (type_identifier) @name) @type)`,
			"structs":   `(type_declaration (type_spec name: (type_identifier) @name type: (struct_type))) @struct`,
			"interfaces": `(type_declaration (type_spec name: (type_identifier) @name type: (interface_type))) @interface`,
			"vars":      `(var_spec (identifier) @name) @var`,
			"consts":    `(const_spec (identifier) @name) @const`,
			"calls":     `(call_expression function: (identifier) @callee) @call`,
			"method_calls": `(call_expression function: (selector_expression field: (field_identifier) @callee)) @call`,
			"imports":   `(import_spec path: (interpreted_string_literal) @import_path) @import`,
		},
	},
	".js": {
		lang: javascript.GetLanguage(),
		queries: map[string]string{
			"functions": `(function_declaration name: (identifier) @name) @func`,
			"classes":   `(class_declaration name: (identifier) @name) @class`,
			"vars":      `(lexical_declaration (variable_declarator (identifier) @name)) @var`,
			"calls":     `(call_expression function: (identifier) @callee) @call`,
			"method_calls": `(call_expression function: (member_expression property: (property_identifier) @callee)) @call`,
			"imports":   `(import_statement source: (string) @import_path) @import`,
		},
	},
	".jsx": {
		lang: javascript.GetLanguage(),
		queries: map[string]string{
			"functions": `(function_declaration name: (identifier) @name) @func`,
			"classes":   `(class_declaration name: (identifier) @name) @class`,
			"calls":     `(call_expression function: (identifier) @callee) @call`,
		},
	},
	".ts": {
		lang: typescript.GetLanguage(),
		queries: map[string]string{
			"functions": `(function_declaration name: (identifier) @name) @func`,
			"classes":   `(class_declaration name: (type_identifier) @name) @class`,
			"interfaces": `(interface_declaration name: (type_identifier) @name) @interface`,
			"vars":      `(lexical_declaration (variable_declarator (identifier) @name)) @var`,
			"calls":     `(call_expression function: (identifier) @callee) @call`,
			"method_calls": `(call_expression function: (member_expression property: (property_identifier) @callee)) @call`,
			"imports":   `(import_statement source: (string) @import_path) @import`,
		},
	},
	".tsx": {
		lang: typescript.GetLanguage(),
		queries: map[string]string{
			"functions": `(function_declaration name: (identifier) @name) @func`,
			"classes":   `(class_declaration name: (type_identifier) @name) @class`,
			"calls":     `(call_expression function: (identifier) @callee) @call`,
		},
	},
	".py": {
		lang: python.GetLanguage(),
		queries: map[string]string{
			"functions": `(function_definition name: (identifier) @name) @func`,
			"classes":   `(class_definition name: (identifier) @name) @class`,
			"calls":     `(call function: (identifier) @callee) @call`,
			"method_calls": `(call function: (attribute attribute: (identifier) @callee)) @call`,
			"imports":   `(import_statement name: (dotted_name) @import_path) @import`,
			"import_from": `(import_from_statement module_name: (dotted_name) @import_path) @import`,
		},
	},
	".rs": {
		lang: rust.GetLanguage(),
		queries: map[string]string{
			"functions": `(function_item name: (identifier) @name) @func`,
			"structs":   `(struct_item name: (type_identifier) @name) @struct`,
			"enums":     `(enum_item name: (type_identifier) @name) @enum`,
			"traits":    `(trait_item name: (type_identifier) @name) @trait`,
			"impls":     `(impl_item type: (type_identifier) @name) @impl`,
			"calls":     `(call_expression function: (identifier) @callee) @call`,
			"method_calls": `(call_expression function: (field_expression field: (field_identifier) @callee)) @call`,
			"uses":      `(use_wildcard) @use`,
		},
	},
}

// Parser extracts code symbols from source files using tree-sitter.
type Parser struct {
	parsers map[string]*sitter.Parser
}

// NewParser creates a new tree-sitter parser.
func NewParser() *Parser {
	p := &Parser{
		parsers: make(map[string]*sitter.Parser),
	}
	// Pre-create parsers for supported languages
	for ext, cfg := range supportedLangs {
		parser := sitter.NewParser()
		parser.SetLanguage(cfg.lang)
		p.parsers[ext] = parser
	}
	return p
}

// ParseFile parses a source file and extracts nodes and edges.
func (p *Parser) ParseFile(path string, content []byte) ([]*Node, []*Edge) {
	ext := filepath.Ext(path)
	cfg, ok := supportedLangs[ext]
	if !ok {
		return nil, nil
	}

	parser, ok := p.parsers[ext]
	if !ok {
		return nil, nil
	}

	tree, err := parser.ParseCtx(context.Background(), nil, content)
	if err != nil || tree == nil {
		return nil, nil
	}
	defer tree.Close()

	root := tree.RootNode()
	var nodes []*Node
	var edges []*Edge

	// Extract package/module info
	pkgPath := extractPackagePath(root, path, content, ext)

	// Track function/method definitions for resolving call edge sources.
	// key = startLine, value = index into nodes slice.
	funcByLine := make(map[int]int)

	// First pass: extract definitions (functions, classes, structs, etc.)
	for queryName, pattern := range cfg.queries {
		// Skip call/import queries in the first pass
		if strings.Contains(queryName, "call") || strings.HasPrefix(queryName, "import") {
			continue
		}

		q, err := sitter.NewQuery([]byte(pattern), cfg.lang)
		if err != nil {
			continue
		}
		qc := sitter.NewQueryCursor()
		qc.Exec(q, root)

		for {
			m, ok := qc.NextMatch()
			if !ok {
				break
			}

			var nameNode, mainNode *sitter.Node
			for _, c := range m.Captures {
				capName := q.CaptureNameForId(c.Index)
				if capName == "name" {
					nameNode = c.Node
				}
				if capName == queryName || capName == "func" || capName == "method" ||
					capName == "class" || capName == "struct" || capName == "interface" ||
					capName == "enum" || capName == "trait" || capName == "impl" ||
					capName == "type" || capName == "var" || capName == "const" {
					mainNode = c.Node
				}
			}

			if mainNode == nil || nameNode == nil {
				continue
			}

			startLine := int(mainNode.StartPoint().Row) + 1
			endLine := int(mainNode.EndPoint().Row) + 1
			name := nameNode.Content(content)

			// If a node with the same name and file already exists, prefer more specific kinds
			// (struct/interface/class > type)
			dup := false
			for _, existing := range nodes {
				if existing.Name == name && existing.File == path {
					if existing.Kind == KindType && (queryName == "structs" || queryName == "interfaces") {
						existing.Kind = map[string]NodeKind{"structs": KindStruct, "interfaces": KindInterface}[queryName]
					}
					dup = true
					break
				}
			}
			if dup {
				continue
			}

			switch {
			case queryName == "functions" || queryName == "methods":
				kind := KindFunc
				if queryName == "methods" {
					kind = KindMethod
				}
				sig := extractSignature(mainNode, content)
				doc := extractDoc(mainNode, content)
				idx := len(nodes)
				nodes = append(nodes, &Node{
					Name:      name,
					Kind:      kind,
					File:      path,
					Line:      startLine,
					EndLine:   endLine,
					PkgPath:   pkgPath,
					Signature: sig,
					Doc:       doc,
				})
				funcByLine[startLine] = idx

			case queryName == "classes":
				nodes = append(nodes, &Node{
					Name:    name,
					Kind:    KindClass,
					File:    path,
					Line:    startLine,
					EndLine: endLine,
					PkgPath: pkgPath,
				})

			case queryName == "structs":
				nodes = append(nodes, &Node{
					Name:    name,
					Kind:    KindStruct,
					File:    path,
					Line:    startLine,
					EndLine: endLine,
					PkgPath: pkgPath,
				})

			case queryName == "interfaces":
				nodes = append(nodes, &Node{
					Name:    name,
					Kind:    KindInterface,
					File:    path,
					Line:    startLine,
					EndLine: endLine,
					PkgPath: pkgPath,
				})

			case queryName == "types":
				nodes = append(nodes, &Node{
					Name:    name,
					Kind:    KindType,
					File:    path,
					Line:    startLine,
					EndLine: endLine,
					PkgPath: pkgPath,
				})

			case queryName == "enums":
				nodes = append(nodes, &Node{
					Name:    name,
					Kind:    KindType,
					File:    path,
					Line:    startLine,
					EndLine: endLine,
					PkgPath: pkgPath,
				})

			case queryName == "traits":
				nodes = append(nodes, &Node{
					Name:    name,
					Kind:    KindInterface,
					File:    path,
					Line:    startLine,
					EndLine: endLine,
					PkgPath: pkgPath,
				})

			case queryName == "impls":
				nodes = append(nodes, &Node{
					Name:    name,
					Kind:    KindType,
					File:    path,
					Line:    startLine,
					EndLine: endLine,
					PkgPath: pkgPath,
				})

			case queryName == "vars":
				nodes = append(nodes, &Node{
					Name:    name,
					Kind:    KindVar,
					File:    path,
					Line:    startLine,
					EndLine: endLine,
					PkgPath: pkgPath,
				})

			case queryName == "consts":
				nodes = append(nodes, &Node{
					Name:    name,
					Kind:    KindConst,
					File:    path,
					Line:    startLine,
					EndLine: endLine,
					PkgPath: pkgPath,
				})
			}
		}
	}

	// Second pass: extract call and import edges
	for queryName, pattern := range cfg.queries {
		if !strings.Contains(queryName, "call") && !strings.HasPrefix(queryName, "import") {
			continue
		}

		q, err := sitter.NewQuery([]byte(pattern), cfg.lang)
		if err != nil {
			continue
		}
		qc := sitter.NewQueryCursor()
		qc.Exec(q, root)

		for {
			m, ok := qc.NextMatch()
			if !ok {
				break
			}

			var nameNode, mainNode *sitter.Node
			for _, c := range m.Captures {
				capName := q.CaptureNameForId(c.Index)
				if capName == "callee" || capName == "import_path" {
					nameNode = c.Node
				}
				if capName == "call" || capName == "import" {
					mainNode = c.Node
				}
			}

			if mainNode == nil || nameNode == nil {
				continue
			}

			startLine := int(mainNode.StartPoint().Row) + 1

			// Find the enclosing function for this call/import
			var fromID int64
			for _, nodeIdx := range funcByLine {
				if nodes[nodeIdx].File == path && nodes[nodeIdx].Line <= startLine && nodes[nodeIdx].EndLine >= startLine {
					// Use the node's assigned ID (0 until graph assigns it; resolveReferences will fix it)
					// We store the node index as a negative marker so resolveReferences can map it
					fromID = int64(-(nodeIdx + 1)) // negative = unresolved, value = node index + 1
					break
				}
			}

			if strings.Contains(queryName, "call") {
				calleeName := nameNode.Content(content)
				edges = append(edges, &Edge{
					Kind:       EdgeCalls,
					FromID:     fromID,
					File:       path,
					Line:       startLine,
					CalleeName: calleeName,
				})
			} else if strings.HasPrefix(queryName, "import") {
				importPath := strings.Trim(nameNode.Content(content), "\"'")
				edges = append(edges, &Edge{
					Kind:       EdgeImports,
					FromID:     fromID,
					File:       path,
					Line:       startLine,
					ImportPath: importPath,
				})
			}
		}
	}

	return nodes, edges
}

// extractPackagePath determines the package/module path for a file.
func extractPackagePath(root *sitter.Node, path string, content []byte, ext string) string {
	switch ext {
	case ".go":
		// Use tree-sitter to find the package clause precisely
		cfg, ok := supportedLangs[ext]
		if ok {
			q, err := sitter.NewQuery([]byte(`(package_clause (package_identifier) @pkg)`), cfg.lang)
			if err == nil {
				qc := sitter.NewQueryCursor()
				qc.Exec(q, root)
				if m, ok := qc.NextMatch(); ok {
					for _, c := range m.Captures {
						return c.Node.Content(content)
					}
				}
			}
		}
		// Fallback to line scanning
		lines := strings.Split(string(content), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "package ") {
				return strings.TrimPrefix(line, "package ")
			}
		}
	case ".py":
		return filepath.Base(filepath.Dir(path))
	case ".rs":
		// Look for mod declarations; fall back to directory name
		cfg, ok := supportedLangs[ext]
		if ok {
			q, err := sitter.NewQuery([]byte(`(mod_item name: (identifier) @mod)`), cfg.lang)
			if err == nil {
				qc := sitter.NewQueryCursor()
				qc.Exec(q, root)
				if m, ok := qc.NextMatch(); ok {
					for _, c := range m.Captures {
						return c.Node.Content(content)
					}
				}
			}
		}
		return filepath.Base(filepath.Dir(path))
	}
	return ""
}

// extractSignature extracts function/method signature.
func extractSignature(node *sitter.Node, content []byte) string {
	// Get the first line or until the body starts
	text := node.Content(content)
	lines := strings.SplitN(text, "\n", 3)
	if len(lines) > 0 {
		sig := strings.TrimSpace(lines[0])
		if len(sig) > 200 {
			sig = sig[:200] + "..."
		}
		return sig
	}
	return ""
}

// extractDoc extracts documentation comments above a node.
func extractDoc(node *sitter.Node, content []byte) string {
	startByte := node.StartByte()
	if startByte == 0 {
		return ""
	}

	before := content[:startByte]
	lines := strings.Split(string(before), "\n")

	var docLines []string
	// Walk backwards to find comments
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}

		// Line comment or hash comment
		if strings.HasPrefix(line, "//") || strings.HasPrefix(line, "#") {
			docLines = append([]string{line}, docLines...)
			continue
		}

		// End of a /* */ block comment — walk backwards to find /*
		if strings.HasSuffix(line, "*/") {
			block := []string{line}
			foundOpen := strings.HasPrefix(line, "/*")
			if !foundOpen {
				for i--; i >= 0; i-- {
					l := strings.TrimSpace(lines[i])
					block = append([]string{l}, block...)
					if strings.HasPrefix(l, "/*") {
						foundOpen = true
						break
					}
				}
			}
			if foundOpen {
				docLines = append(block, docLines...)
			}
			continue
		}

		// Standalone /* (opening of a block comment on its own line)
		if strings.HasPrefix(line, "/*") {
			docLines = append([]string{line}, docLines...)
			continue
		}

		break
	}

	if len(docLines) > 0 {
		doc := strings.Join(docLines, "\n")
		if len(doc) > 500 {
			doc = doc[:500] + "..."
		}
		return doc
	}
	return ""
}
