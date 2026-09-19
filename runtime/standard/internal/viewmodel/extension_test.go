package viewmodel

import (
	"reflect"
	"testing"

	extensionmgr "nekocode/bot/extension"
	"nekocode/bot/extension/mcp"
	"nekocode/bot/extension/plugin"
	"nekocode/bot/extension/skill"
	controlruntime "nekocode/runtime"
)

func TestExtensionCombinesSourcesAndHealth(t *testing.T) {
	plugins := []*plugin.Plugin{{
		Manifest: plugin.Manifest{
			Name: "demo",
			MCPServers: map[string]plugin.MCPServerConfig{
				"plugin-server": {Command: "plugin-mcp"},
			},
		},
		Enabled: true,
	}}
	got := Extension(extensionmgr.Snapshot{
		Plugins:     plugins,
		Agents:      []extensionmgr.AgentInfo{{ID: "demo/reviewer", Name: "reviewer", Plugin: "demo"}},
		AgentErrors: map[string]string{"demo": "invalid agent"},
		MCPHealth: map[string]mcp.Health{
			"plugin-server": {Status: mcp.StatusReady, ToolCount: 2},
		},
		ConfiguredMCP: []extensionmgr.ConfiguredMCP{{Name: "configured-server", Source: "配置", Command: "configured-mcp", Enabled: true}},
	})

	if len(got.Plugins) != 1 || len(got.MCP) != 2 {
		t.Fatalf("extension view = %+v", got)
	}
	if got.Plugins[0].AgentError != "invalid agent" || !reflect.DeepEqual(got.Plugins[0].ActiveAgents, []string{"demo/reviewer"}) {
		t.Fatalf("agent state missing: %+v", got.Plugins[0])
	}
	servers := make(map[string]controlruntime.MCPServerView, len(got.MCP))
	for _, server := range got.MCP {
		servers[server.Name] = server
	}
	if server := servers["plugin-server"]; server.Status != mcp.StatusReady || server.ToolCount != 2 {
		t.Fatalf("plugin server health = %+v", server)
	}
	if server := servers["configured-server"]; server.Plugin != "配置" || !server.PluginEnabled {
		t.Fatalf("configured server = %+v", server)
	}
}

func TestBuildPluginViews(t *testing.T) {
	plugins := []*plugin.Plugin{{
		Manifest: plugin.Manifest{
			Name:     "demo",
			Version:  "1.2.0",
			Skills:   []string{"skills/a"},
			Agents:   []string{"agents/x.md"},
			Commands: []plugin.CommandEntry{{Name: "run", Source: "run.sh"}},
			MCPServers: map[string]plugin.MCPServerConfig{
				"srv1": {Command: "node", Args: []string{"s.js"}},
			},
		},
		Dir:     "/plugins/demo",
		Enabled: true,
	}}

	got := pluginViews(plugins)
	if len(got) != 1 {
		t.Fatalf("expected 1 view, got %d", len(got))
	}
	v := got[0]
	if v.Name != "demo" || v.Version != "1.2.0" || !v.Enabled {
		t.Fatalf("base fields mismatch: %+v", v)
	}
	if !reflect.DeepEqual(v.Skills, []string{"/plugins/demo/skills/a"}) {
		t.Fatalf("skill dirs mismatch: %+v", v.Skills)
	}
	if !reflect.DeepEqual(v.SkillNames, []string{"a"}) {
		t.Fatalf("skill names mismatch: %+v", v.SkillNames)
	}
	if !reflect.DeepEqual(v.Agents, []string{"x.md"}) {
		t.Fatalf("agent names mismatch: %+v", v.Agents)
	}
	if !reflect.DeepEqual(v.Commands, []string{"run"}) {
		t.Fatalf("command names mismatch: %+v", v.Commands)
	}
	if !reflect.DeepEqual(v.MCPServers, []string{"srv1"}) {
		t.Fatalf("mcp server names mismatch: %+v", v.MCPServers)
	}
}

func TestBuildPluginMCPViews(t *testing.T) {
	plugins := []*plugin.Plugin{{
		Manifest: plugin.Manifest{
			Name: "demo",
			MCPServers: map[string]plugin.MCPServerConfig{
				"srv1": {Command: "node", Args: []string{"s.js"}},
				"srv2": {Command: "python", Args: []string{"-m", "srv"}},
				"srv3": {Command: "${CLAUDE_PLUGIN_ROOT}/mcp/neko-devkit-mcp", Args: []string{"${PLUGIN_ROOT}"}},
			},
		},
		Dir:     "/plugins/demo",
		Enabled: true,
	}}

	got := pluginMCPViews(plugins)
	if len(got) != 3 {
		t.Fatalf("expected 3 mcp views, got %d", len(got))
	}
	byName := map[string]controlruntime.MCPServerView{}
	for _, s := range got {
		byName[s.Name] = s
	}
	s1, ok := byName["srv1"]
	if !ok {
		t.Fatalf("missing srv1")
	}
	if s1.Plugin != "demo" || s1.Command != "node" || !s1.PluginEnabled {
		t.Fatalf("srv1 mismatch: %+v", s1)
	}
	if !reflect.DeepEqual(s1.Args, []string{"s.js"}) {
		t.Fatalf("srv1 args mismatch: %+v", s1.Args)
	}
	if s2 := byName["srv2"]; !reflect.DeepEqual(s2.Args, []string{"-m", "srv"}) {
		t.Fatalf("srv2 args mismatch: %+v", s2.Args)
	}
	if s3 := byName["srv3"]; s3.Command != "/plugins/demo/mcp/neko-devkit-mcp" {
		t.Fatalf("srv3 command mismatch: %+v", s3.Command)
	}
	if s3 := byName["srv3"]; !reflect.DeepEqual(s3.Args, []string{"/plugins/demo"}) {
		t.Fatalf("srv3 args mismatch: %+v", s3.Args)
	}
}

func TestEffectiveProjectMCPView(t *testing.T) {
	snapshot := extensionmgr.Snapshot{
		ConfiguredMCP: []extensionmgr.ConfiguredMCP{
			{Name: "same", Source: "/project/.nekocode/.mcp.json", Command: "project", Enabled: false},
			{Name: "local", Source: "/project/.nekocode/.mcp.json", Command: "local", Enabled: true},
		},
		MCPHealth: map[string]mcp.Health{"local": {Status: mcp.StatusReady, ToolCount: 1}},
	}
	got := Extension(snapshot)
	if len(got.MCP) != 2 || got.MCP[0].Command != "project" || got.MCP[0].Status != "disabled" || got.MCP[1].Status != mcp.StatusReady {
		t.Fatalf("project definitions not reflected: %+v", got.MCP)
	}
}

func TestShadowedPluginDoesNotInheritHostHealth(t *testing.T) {
	snapshot := extensionmgr.Snapshot{
		Plugins:       []*plugin.Plugin{{Manifest: plugin.Manifest{Name: "demo", MCPServers: map[string]plugin.MCPServerConfig{"same": {Command: "plugin-command"}}}, Enabled: true}},
		ConfiguredMCP: []extensionmgr.ConfiguredMCP{{Name: "same", Source: "/project/.nekocode/.mcp.json", Command: "project-command", Enabled: true}},
		MCPHealth:     map[string]mcp.Health{"same": {Owner: "config:same", Status: mcp.StatusReady, ToolCount: 3}},
	}
	got := Extension(snapshot)
	if len(got.MCP) != 2 || got.MCP[0].Status != "shadowed" || got.MCP[0].ToolCount != 0 || got.MCP[1].Status != mcp.StatusReady {
		t.Fatalf("shadowed plugin inherited host state: %+v", got.MCP)
	}
}

func TestBuildPluginMCPViewsEmpty(t *testing.T) {
	plugins := []*plugin.Plugin{{Manifest: plugin.Manifest{Name: "bare"}, Dir: "/p"}}
	if got := pluginMCPViews(plugins); len(got) != 0 {
		t.Fatalf("expected empty mcp, got %+v", got)
	}
}

func TestBaseNamesFiltersEmpty(t *testing.T) {
	got := baseNames([]string{"", "/x/y", ""})
	if !reflect.DeepEqual(got, []string{"y"}) {
		t.Fatalf("expected empty paths filtered, got %+v", got)
	}
}

func TestBuildSkillViews(t *testing.T) {
	skills := []*skill.Skill{
		{Name: "builtin", Description: "built in"},
		{Name: "plugin-skill", Description: "from plugin", Dir: "/plugins/p/skills/s1", Files: []string{"b", "a"}},
	}
	loaded := map[string]bool{"builtin": true}
	plugins := []controlruntime.PluginView{{
		Name:   "p",
		Skills: []string{"/plugins/p/skills"},
	}}

	got := skillViews(skills, loaded, plugins)
	if len(got) != 2 {
		t.Fatalf("unexpected views: %+v", got)
	}
	if got[0].Name != "builtin" || !got[0].Loaded || got[0].Source != "内置" || got[0].SourceKind != "builtin" {
		t.Fatalf("builtin skill mismatch: %+v", got[0])
	}
	if got[1].Name != "plugin-skill" || got[1].Loaded || got[1].Source != "插件" || got[1].SourceKind != "plugin" || got[1].Plugin != "p" {
		t.Fatalf("plugin skill mismatch: %+v", got[1])
	}
	if !reflect.DeepEqual(got[1].Files, []string{"a", "b"}) {
		t.Fatalf("files not sorted: %+v", got[1].Files)
	}
}

func TestSourceForDirKinds(t *testing.T) {
	plugins := []controlruntime.PluginView{{Name: "p", Skills: []string{"/plugins/p/skills"}}}
	cases := []struct {
		dir       string
		wantKind  string
		wantLabel string
		wantPlug  string
	}{
		{"", "builtin", "内置", ""},
		{"/plugins/p/skills/s1", "plugin", "插件", "p"},
		{"/plugins/p/skills", "plugin", "插件", "p"},
		{"/home/me/.nekocode/skills/foo", "local", "本地", ""},
	}
	for _, c := range cases {
		kind, label, plug := sourceForDir(c.dir, plugins)
		if kind != c.wantKind || label != c.wantLabel || plug != c.wantPlug {
			t.Errorf("sourceForDir(%q) = (%q,%q,%q), want (%q,%q,%q)", c.dir, kind, label, plug, c.wantKind, c.wantLabel, c.wantPlug)
		}
	}
}
