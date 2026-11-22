package prompt

import (
	"fmt"
	"html"
	"path/filepath"
	"strings"
	"unicode"
)

// FormatEnvironment renders volatile facts that the model needs in order to
// resolve paths and choose an authorized capability. Runtime implementation
// details belong to tool schemas and permission enforcement, not this block.
func FormatEnvironment(env Environment, date, shell, goos, goarch string) string {
	if env.Cwd == "" && len(env.Roots) == 0 && env.ManagedProcesses == "" && env.MCPCapabilities == "" && date == "" && shell == "" && goos == "" && goarch == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("<environment_context>\n")
	if env.Cwd != "" {
		fmt.Fprintf(&b, "<cwd>%s</cwd>\n", escapeEnvValue(env.Cwd))
	}
	writeEnvField(&b, "shell", shell)
	writeEnvField(&b, "current_date", date)
	writeEnvField(&b, "os", goos)
	writeEnvField(&b, "arch", goarch)
	if len(env.Roots) > 0 {
		b.WriteString("<workspace_roots>\n")
		// A saved tool result can have its own read root under the session
		// output directory. Listing each such file makes every worker's
		// environment change when a sibling saves output.
		grants := make(map[Root]bool, len(env.Roots))
		for _, root := range env.Roots {
			grants[root] = true
		}
		for _, root := range env.Roots {
			if coveredRoot(root, grants) {
				continue
			}
			element := "root"
			if root.FileOnly {
				element = "file"
			}
			fmt.Fprintf(&b, "  <%s access=\"%s\">%s</%s>\n",
				element, escapeEnvValue(root.Access), escapeEnvValue(root.Path), element)
		}
		b.WriteString("</workspace_roots>\n")
	}
	if strings.TrimSpace(env.ManagedProcesses) != "" {
		fmt.Fprintf(&b, "<managed_processes>%s</managed_processes>\n", escapeEnvValue(env.ManagedProcesses))
	}
	writeEnvField(&b, "mcp_capabilities", env.MCPCapabilities)
	b.WriteString("</environment_context>")
	return b.String()
}

// Compare path components and equal access only. Authority checks still use
// the full workspace snapshot, including nested grants of a different kind.
func coveredRoot(root Root, grants map[Root]bool) bool {
	if !filepath.IsAbs(root.Path) || filepath.Clean(root.Path) != root.Path {
		return false
	}
	for path := root.Path; ; {
		parent := filepath.Dir(path)
		if parent == path {
			return false
		}
		if grants[Root{Path: parent, Access: root.Access}] {
			return true
		}
		path = parent
	}
}

func writeEnvField(b *strings.Builder, name, value string) {
	if value != "" {
		fmt.Fprintf(b, "<%s>%s</%s>\n", name, escapeEnvValue(value), name)
	}
}

// escapeEnvValue prevents paths or OS metadata from closing the XML-like
// delimiter and smuggling instructions into a system prompt. Control
// characters are replaced because they have no useful representation here.
func escapeEnvValue(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return '\ufffd'
		}
		return r
	}, value)
	return html.EscapeString(value)
}
