package agentprofile

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	goyaml "gopkg.in/yaml.v3"
	"nekocode/util/yaml"
)

var validName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// Load opens through os.Root so symlink replacement cannot escape the plugin.
// MCP targets are checked at invocation because servers start asynchronously.
func Load(root, path string, hasTool func(string) bool) (Profile, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Profile{}, err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return Profile{}, err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return Profile{}, fmt.Errorf("agent path is outside plugin root: %s", path)
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return Profile{}, err
	}
	defer dir.Close()
	// Reject static special files before Open (opening a FIFO could block).
	const maxSize = 1 << 20
	info, err := dir.Stat(rel)
	if err != nil {
		return Profile{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxSize {
		return Profile{}, fmt.Errorf("agent must be a regular file no larger than 1 MiB")
	}
	file, err := dir.Open(rel)
	if err != nil {
		return Profile{}, err
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return Profile{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxSize {
		return Profile{}, fmt.Errorf("agent must be a regular file no larger than 1 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxSize+1))
	if err != nil {
		return Profile{}, err
	}
	if len(data) > maxSize {
		return Profile{}, fmt.Errorf("agent exceeds 1 MiB")
	}
	return parse(string(data), hasTool)
}

func parse(content string, hasTool func(string) bool) (Profile, error) {
	header, body, err := yaml.ParseYAMLFrontmatter(content)
	if err != nil {
		return Profile{}, err
	}
	var def struct {
		Name        string    `yaml:"name"`
		Description string    `yaml:"description"`
		Base        string    `yaml:"base"`
		Tools       toolNames `yaml:"tools"`
		Skills      []string  `yaml:"skills"`
		MaxSteps    int       `yaml:"max_steps"`
	}
	decoder := goyaml.NewDecoder(strings.NewReader(string(header)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&def); err != nil {
		return Profile{}, fmt.Errorf("agent frontmatter: %w", err)
	}
	if !validName.MatchString(def.Name) {
		return Profile{}, fmt.Errorf("invalid agent name %q: use letters, digits, hyphens or underscores", def.Name)
	}
	if def.MaxSteps < 0 || def.MaxSteps > MaxSteps {
		return Profile{}, fmt.Errorf("max_steps must be between 0 and %d", MaxSteps)
	}
	p := Profile{Name: def.Name, Description: strings.TrimSpace(def.Description), SystemPrompt: strings.TrimSpace(body), Tools: []string(def.Tools), Skills: def.Skills, MaxSteps: def.MaxSteps}
	if p.SystemPrompt == "" {
		return Profile{}, fmt.Errorf("agent instructions cannot be empty")
	}
	if def.Base != "" {
		var base *Profile
		for _, candidate := range Builtins() {
			if candidate.Name == def.Base {
				base = &candidate
				break
			}
		}
		if base == nil {
			return Profile{}, fmt.Errorf("unknown base profile %q", def.Base)
		}
		if def.Tools == nil {
			p.Tools = base.Tools
		}
		for _, name := range p.Tools {
			if !slices.Contains(base.Tools, name) {
				return Profile{}, fmt.Errorf("tool %q exceeds base profile %q", name, def.Base)
			}
		}
	}
	for _, name := range p.Tools {
		if name == "task" || name == "agent_profiles" || name == "nekocode_submit_result" {
			return Profile{}, fmt.Errorf("reserved agent tool %q", name)
		}
		if strings.HasPrefix(name, "mcp__") {
			parts := strings.Split(strings.TrimPrefix(name, "mcp__"), "__")
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.ContainsAny(name, "*? \t\n") || !slices.Contains(p.Tools, "capability") {
				return Profile{}, fmt.Errorf("MCP target %q requires an exact server/tool name and capability in tools", name)
			}
		} else if hasTool == nil || !hasTool(name) {
			return Profile{}, fmt.Errorf("unknown agent tool %q", name)
		}
	}
	for _, name := range p.Skills {
		if strings.TrimSpace(name) == "" {
			return Profile{}, fmt.Errorf("skill names cannot be empty")
		}
	}
	return p, nil
}

// Accept comma-separated Claude tool names as well as YAML sequences.
type toolNames []string

func (names *toolNames) UnmarshalYAML(node *goyaml.Node) error {
	var raw []string
	if node.Kind == goyaml.ScalarNode && node.Tag == "!!str" {
		raw = strings.Split(node.Value, ",")
	} else if err := node.Decode(&raw); err != nil {
		return err
	}
	out := make([]string, 0, len(raw))
	aliases := map[string]string{"Read": "read", "Write": "write", "Edit": "edit", "Bash": "shell", "Grep": "grep", "Glob": "glob", "LS": "list", "WebSearch": "web_search", "WebFetch": "web_fetch"}
	for _, name := range raw {
		name = strings.TrimSpace(name)
		if mapped, ok := aliases[name]; ok {
			name = mapped
		}
		if name == "" {
			return fmt.Errorf("tool names cannot be empty")
		}
		if !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	*names = out
	return nil
}
