package runner

import (
	"path/filepath"
	"strings"

	"nekocode/bot/extension/tool/runtime/permission"

	"mvdan.cc/sh/v3/syntax"
)

func bashRememberRules(toolName, cmd string) []permission.Rule {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return nil
	}
	if permission.ClassifyShellStructure(cmd).Dynamic() {
		return []permission.Rule{{Tool: toolName, Literal: cmd, Effect: permission.EffectAllow}}
	}
	specs := bashRememberSpecs(cmd)
	rules := make([]permission.Rule, 0, len(specs))
	for _, spec := range specs {
		rules = append(rules, permission.Rule{Tool: toolName, Specifier: spec, Effect: permission.EffectAllow})
	}
	return rules
}

func bashRememberSpecs(cmd string) []string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return nil
	}
	parser := syntax.NewParser()
	file, err := parser.Parse(strings.NewReader(cmd), "")
	if err != nil {
		return []string{cmd}
	}
	seen := map[string]bool{}
	var specs []string
	syntax.Walk(file, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		name := literalShellWord(call.Args[0])
		if name == "" {
			return true
		}
		spec := name
		if len(call.Args) > 1 {
			spec = name + " *"
		}
		if !seen[spec] {
			seen[spec] = true
			specs = append(specs, spec)
		}
		return true
	})
	if len(specs) == 0 {
		return []string{cmd}
	}
	return specs
}

func literalShellWord(w *syntax.Word) string {
	if w == nil {
		return ""
	}
	var b strings.Builder
	for _, part := range w.Parts {
		lit, ok := part.(*syntax.Lit)
		if !ok {
			return ""
		}
		b.WriteString(lit.Value)
	}
	return b.String()
}

// pathRememberSpec derives a path specifier to remember for file tools.
// Workspace-relative paths stay relative ("/src/foo.go" → "/src/**" is too
// broad; instead remember the file's parent dir: "/src/**"). Home paths get
// "~/" prefix; absolute paths get "//".
func pathRememberSpec(p, workspace, home string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if !filepath.IsAbs(p) && workspace != "" {
		p = filepath.Join(workspace, p)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	rel := ""
	if workspace != "" {
		if r, err := filepath.Rel(workspace, abs); err == nil && !strings.HasPrefix(r, "..") {
			rel = "/" + filepath.ToSlash(r)
		}
	}
	if rel != "" {
		// Remember the parent directory as a writable tree. A file in the
		// workspace root stays exact; broadening it to "/" would allow the
		// entire workspace.
		dir := filepath.ToSlash(filepath.Dir(rel))
		if dir == "/." || dir == "/" {
			return rel
		}
		return dir + "/**"
	}
	if home != "" && strings.HasPrefix(abs, home+"/") {
		return "~/" + filepath.ToSlash(strings.TrimPrefix(abs, home+"/"))
	}
	return "//" + filepath.ToSlash(abs)
}
