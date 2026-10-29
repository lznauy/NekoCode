package extension

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	goyaml "gopkg.in/yaml.v3"
	"nekocode/bot/extension/agentprofile"
	"nekocode/bot/extension/mcp"
	"nekocode/bot/extension/plugin"
	"nekocode/bot/extension/skill"
	"nekocode/bot/policy"
	utilhttp "nekocode/util/http"
	"nekocode/util/yaml"
)

const (
	maxPluginDiagnosticPathBytes        = 1024
	maxPluginDiagnosticMessageBytes     = 4096
	maxPluginCheckDiagnosticBytes       = 64 * 1024
	maxPluginCheckReportDiagnosticBytes = 256 * 1024
	maxPluginDiagnostics                = 255
	pluginReportIncompleteMessage       = "report is incomplete"
)

type PluginDiagnostic struct {
	Level   string `json:"level"`
	Path    string `json:"path"`
	Message string `json:"message"`
}
type PluginCheck struct {
	Name        string             `json:"name"`
	Enabled     bool               `json:"enabled"`
	Diagnostics []PluginDiagnostic `json:"diagnostics"`
}

// CheckPlugins performs static inspection, including disabled plugins. It never
// reloads the registry, executes plugin code or starts an MCP transport.
func (m *Manager) CheckPlugins(ctx context.Context, name string) ([]PluginCheck, error) {
	m.ops.Lock()
	defer m.ops.Unlock()
	m.mu.Lock()
	plugins := m.plugins.PluginSnapshots()
	var hasTool func(string) bool
	if m.tools != nil {
		hasTool = m.tools.Has
	}
	m.mu.Unlock()
	var results []PluginCheck
	for _, p := range plugins {
		if name != "" && p.Name != name {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		results = append(results, checkPlugin(ctx, p, hasTool))
	}
	if name != "" && len(results) == 0 {
		return nil, fmt.Errorf("plugin not found: %s", name)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Name < results[j].Name })
	results = limitPluginDiagnosticBytes(results, maxPluginCheckReportDiagnosticBytes, false)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, nil
}
func checkPlugin(ctx context.Context, installed *plugin.Plugin, hasTool func(string) bool) (result PluginCheck) {
	result = PluginCheck{Name: installed.Name, Enabled: installed.Enabled}
	incomplete := false
	defer func() {
		checks := limitPluginDiagnosticBytes([]PluginCheck{result}, maxPluginCheckDiagnosticBytes, incomplete)
		result = checks[0]
	}()
	add := func(level, path, message string) {
		if len(result.Diagnostics) >= maxPluginDiagnostics {
			incomplete = true
			return
		}
		result.Diagnostics = append(result.Diagnostics, PluginDiagnostic{level, path, message})
	}
	root, err := os.OpenRoot(installed.Dir)
	if err != nil {
		add("error", ".", err.Error())
		return result
	}
	defer root.Close()
	read := func(path string) ([]byte, error) {
		info, err := root.Stat(path)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() > 1<<20 {
			return nil, fmt.Errorf("must be a regular file no larger than 1 MiB")
		}
		file, err := root.Open(path)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		info, err = file.Stat()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() > 1<<20 {
			return nil, fmt.Errorf("must be a regular file no larger than 1 MiB")
		}
		data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
		if len(data) > 1<<20 {
			return nil, fmt.Errorf("file exceeds 1 MiB")
		}
		return data, err
	}
	relative := func(path string) (string, error) {
		rel, err := filepath.Rel(installed.Dir, path)
		if err != nil || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("path is outside plugin root")
		}
		return rel, nil
	}
	data, err := read(filepath.Join(".claude-plugin", "plugin.json"))
	if err != nil {
		add("error", ".claude-plugin/plugin.json", err.Error())
		return result
	}
	manifest, err := plugin.ParseManifestData(data)
	if err != nil {
		add("error", ".claude-plugin/plugin.json", err.Error())
		return result
	}
	if manifest.Name != installed.Name {
		add("error", ".claude-plugin/plugin.json", "name differs from installed registry; reload after correcting it")
	}
	for _, key := range unknownJSONFields(data, reflect.TypeFor[plugin.Manifest]()) {
		add("warning", ".claude-plugin/plugin.json", "unsupported manifest field: "+key)
	}
	p := &plugin.Plugin{Manifest: *manifest, Dir: installed.Dir}
	files, discoveryErr := discoverPluginCheckFiles(ctx, root, p)
	if discoveryErr != nil {
		add("error", ".", "component discovery incomplete: "+discoveryErr.Error())
	}
	inspectMCPFields := func(data []byte, path string) {
		var config map[string]json.RawMessage
		if json.Unmarshal(data, &config) != nil {
			return
		}
		var servers map[string]json.RawMessage
		if json.Unmarshal(config["mcpServers"], &servers) != nil {
			return
		}
		for _, name := range slices.Sorted(maps.Keys(servers)) {
			for _, key := range unknownJSONFields(servers[name], reflect.TypeFor[mcp.ServerConfig]()) {
				add("warning", path, "ignored MCP field in server "+name+": "+key)
			}
		}
	}
	inspectMCPFields(data, ".claude-plugin/plugin.json")
	if len(p.Commands) > 0 {
		add("warning", ".claude-plugin/plugin.json", "commands declarations are not registered; migrate workflows to skills")
	}
	if _, err := root.Stat("commands"); err == nil {
		add("warning", "commands", "legacy command directory is not loaded; migrate workflows to skills")
	}
	if _, err := root.Stat("install.sh"); err == nil {
		add("warning", "install.sh", "installation script is not executed automatically; review dependencies manually")
	}
	// Discovery follows the same declared roots and first-name-wins behavior as runtime.
	seenSkills := map[string]bool{}
	filesSeen := map[string]bool{}
	visits := 0
	for _, dir := range files.skills {
		rel, err := relative(dir)
		if err != nil {
			add("error", dir, err.Error())
			continue
		}
		if info, err := root.Lstat(rel); err == nil && info.Mode()&os.ModeSymlink != 0 {
			if target, err := root.Stat(rel); err == nil && target.IsDir() {
				add("warning", rel, "symlink skill directories are not discovered at runtime")
				continue
			}
		}
		found := 0
		err = fs.WalkDir(root.FS(), filepath.ToSlash(rel), func(path string, entry fs.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			visits++
			if visits > 4096 {
				return fmt.Errorf("skill discovery exceeds 4096 entries")
			}
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.EqualFold(entry.Name(), "SKILL.md") {
				return nil
			}
			found++
			if filesSeen[path] {
				return fs.SkipDir
			}
			filesSeen[path] = true
			data, err := read(filepath.FromSlash(path))
			if err != nil {
				add("error", path, err.Error())
				return fs.SkipDir
			}
			sk, err := skill.ParseContent(string(data))
			if err != nil {
				add("error", path, err.Error())
				return fs.SkipDir
			}
			if seenSkills[sk.Name] {
				add("warning", path, "duplicate skill name is shadowed: "+sk.Name)
			}
			seenSkills[sk.Name] = true
			header, _, _ := yaml.ParseYAMLFrontmatter(string(data))
			var fields map[string]goyaml.Node
			_ = goyaml.Unmarshal(header, &fields)
			known := map[string]bool{"name": true, "description": true, "context": true, "disable-model-invocation": true}
			var keys []string
			for key := range fields {
				if !known[key] {
					keys = append(keys, key)
				}
			}
			sort.Strings(keys)
			for _, key := range keys {
				add("warning", path, "ignored skill field: "+key)
			}
			message := "skill parses successfully"
			if sk.DisableModelInvocation {
				message += " (manual-only)"
			}
			add("ok", path, message)
			return fs.SkipDir
		})
		if err != nil {
			add("error", rel, err.Error())
		} else if found == 0 {
			add("warning", rel, "no SKILL.md found in declared skill root")
		}
	}
	seenAgents := map[string]bool{}
	for _, path := range files.agents {
		if err := ctx.Err(); err != nil {
			add("error", ".", err.Error())
			return result
		}
		rel, err := relative(path)
		if err != nil {
			add("error", path, err.Error())
			continue
		}
		profile, err := agentprofile.Load(p.Dir, path, hasTool)
		if err != nil {
			add("error", rel, err.Error())
			continue
		}
		if seenAgents[profile.Name] {
			add("error", rel, "duplicate agent name: "+profile.Name)
		}
		seenAgents[profile.Name] = true
		if profile.Model != "" {
			add("warning", rel, "model name must exist in host models configuration (not checked here)")
		}
		for _, name := range profile.Skills {
			add("warning", rel, "delegated skill must be available and model-invocable at runtime: "+name)
		}
		add("ok", rel, "agent profile parses successfully")
	}
	if path := files.hooks; path != "" {
		if _, err := policy.LoadPluginHooks(p.Dir, path); err != nil {
			add("error", path, err.Error())
		} else {
			add("ok", path, "hook configuration parses successfully; actions were not executed")
		}
	}
	// Autodiscovery returns the first .mcp.json, even if it is malformed. Inspect
	// its bytes directly so malformed files cannot silently look like no servers.
	servers := p.Manifest.MCPServers
	if len(servers) == 0 {
		mcpPath := files.mcp
		if mcpPath != "" {
			data, err := read(filepath.FromSlash(mcpPath))
			var config struct {
				Servers map[string]mcp.ServerConfig `json:"mcpServers"`
			}
			if err == nil {
				err = json.Unmarshal(data, &config)
				inspectMCPFields(data, mcpPath)
			}
			if err != nil {
				add("error", mcpPath, err.Error())
			} else {
				servers = config.Servers
				if len(servers) == 0 {
					add("warning", mcpPath, "no mcpServers definitions found")
				}
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(servers)) {
		cfg := servers[name]
		path := "mcpServers." + name
		if cfg.URL != "" && cfg.Command != "" {
			add("error", path, "must specify command or URL, not both")
			continue
		}
		if cfg.URL != "" {
			if err := utilhttp.ValidateSecureURL(cfg.URL); err != nil {
				add("error", path, err.Error())
				continue
			}
		} else if strings.TrimSpace(cfg.Command) == "" {
			add("error", path, "requires a command or URL")
			continue
		}
		add("ok", path, "MCP definition parses successfully; dependencies, credentials and connection were not tested")
	}
	sort.SliceStable(result.Diagnostics, func(i, j int) bool {
		a, b := result.Diagnostics[i], result.Diagnostics[j]
		if a.Level != b.Level {
			return diagnosticPriority(a.Level) < diagnosticPriority(b.Level)
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Message < b.Message
	})
	return result
}

// limitPluginDiagnosticBytes budgets JSON-shaped diagnostics, including one
// byte of array punctuation per entry. It always reserves room for the fixed
// incomplete-report error whenever anything is truncated or omitted.
func limitPluginDiagnosticBytes(checks []PluginCheck, limit int, incomplete bool) []PluginCheck {
	used := 2 // Empty JSON array.
	last := -1
	stopped := false
	for i := range checks {
		kept := checks[i].Diagnostics[:0]
		for _, diagnostic := range checks[i].Diagnostics {
			if diagnostic.Level == "error" && diagnostic.Path == "." && diagnostic.Message == pluginReportIncompleteMessage {
				incomplete = true
				continue
			}
			var truncated bool
			diagnostic.Path, truncated = truncateDiagnosticText(diagnostic.Path, maxPluginDiagnosticPathBytes)
			incomplete = incomplete || truncated
			diagnostic.Message, truncated = truncateDiagnosticText(diagnostic.Message, maxPluginDiagnosticMessageBytes)
			incomplete = incomplete || truncated
			if stopped || used+pluginDiagnosticBytes(diagnostic) > limit {
				incomplete = true
				stopped = true
				continue
			}
			kept = append(kept, diagnostic)
			used += pluginDiagnosticBytes(diagnostic)
			last = i
		}
		checks[i].Diagnostics = kept
	}
	if !incomplete || len(checks) == 0 {
		return checks
	}
	sentinel := PluginDiagnostic{Level: "error", Path: ".", Message: pluginReportIncompleteMessage}
	for last >= 0 && (used+pluginDiagnosticBytes(sentinel) > limit || len(checks[last].Diagnostics) >= maxPluginDiagnostics) {
		diagnostics := checks[last].Diagnostics
		if len(diagnostics) == 0 {
			last--
			continue
		}
		removed := diagnostics[len(diagnostics)-1]
		used -= pluginDiagnosticBytes(removed)
		checks[last].Diagnostics = diagnostics[:len(diagnostics)-1]
	}
	if last < 0 {
		last = len(checks) - 1
	}
	checks[last].Diagnostics = append(checks[last].Diagnostics, sentinel)
	sort.SliceStable(checks[last].Diagnostics, func(i, j int) bool {
		a, b := checks[last].Diagnostics[i], checks[last].Diagnostics[j]
		if a.Level != b.Level {
			return diagnosticPriority(a.Level) < diagnosticPriority(b.Level)
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Message < b.Message
	})
	return checks
}

func pluginDiagnosticBytes(diagnostic PluginDiagnostic) int {
	encoded, _ := json.Marshal(diagnostic)
	return len(encoded) + 1
}

func truncateDiagnosticText(text string, limit int) (string, bool) {
	valid := strings.ToValidUTF8(text, "\uFFFD")
	truncated := valid != text
	if len(valid) <= limit {
		return valid, truncated
	}
	end := limit
	for end > 0 && !utf8.RuneStart(valid[end]) {
		end--
	}
	return valid[:end], true
}

func unknownJSONFields(data []byte, t reflect.Type) []string {
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil {
		return nil
	}
	known := map[string]bool{}
	for i := 0; i < t.NumField(); i++ {
		tag := strings.Split(t.Field(i).Tag.Get("json"), ",")[0]
		known[tag] = true
	}
	var result []string
	for name := range fields {
		if !known[name] {
			result = append(result, name)
		}
	}
	sort.Strings(result)
	return result
}
func (m *Manager) pluginCheckReport(ctx context.Context, args []string) string {
	name := strings.Join(args, " ")
	checks, err := m.CheckPlugins(ctx, name)
	if err != nil {
		return "Plugin check failed: " + err.Error()
	}
	if len(checks) == 0 {
		return "No installed plugins to check."
	}
	var out strings.Builder
	out.WriteString("Static compatibility check (no plugin activation, hook execution or MCP connection):\n")
	for _, check := range checks {
		fmt.Fprintf(&out, "\n%s (enabled=%v)\n", check.Name, check.Enabled)
		errors, warnings := 0, 0
		for _, d := range check.Diagnostics {
			if d.Level == "error" {
				errors++
			}
			if d.Level == "warning" {
				warnings++
			}
		}
		fmt.Fprintf(&out, "  Result: %d error(s), %d warning(s). Runtime availability is not verified.\n", errors, warnings)
		for _, d := range check.Diagnostics {
			fmt.Fprintf(&out, "  [%s] %s: %s\n", d.Level, d.Path, d.Message)
		}
	}
	return out.String()
}

func diagnosticPriority(level string) int {
	switch level {
	case "error":
		return 0
	case "warning":
		return 1
	default:
		return 2
	}
}
