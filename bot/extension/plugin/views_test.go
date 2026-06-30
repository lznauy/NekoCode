package plugin

import (
	"reflect"
	"testing"

	"nekocode/common"
)

func TestViewBundleFields(t *testing.T) {
	p := &Plugin{
		Manifest: Manifest{
			Name:     "demo",
			Version:  "1.2.0",
			Skills:   []string{"skills/a"},
			Agents:   []string{"agents/x.md"},
			Commands: []CommandEntry{{Name: "run", Source: "run.sh"}},
			MCPServers: map[string]MCPServerConfig{
				"srv1": {Command: "node", Args: []string{"s.js"}, DangerLevel: "high"},
			},
		},
		Dir:     "/plugins/demo",
		Enabled: true,
	}

	got := viewFor(p)
	if got.Name != "demo" || got.Version != "1.2.0" || !got.Enabled {
		t.Fatalf("base fields mismatch: %+v", got)
	}
	if len(got.Skills) != 1 || got.Skills[0] != "/plugins/demo/skills/a" {
		t.Fatalf("skill dirs mismatch: %+v", got.Skills)
	}
	if len(got.SkillNames) != 1 || got.SkillNames[0] != "a" {
		t.Fatalf("skill names mismatch: %+v", got.SkillNames)
	}
	if len(got.Agents) != 1 || got.Agents[0] != "x.md" {
		t.Fatalf("agent names mismatch: %+v", got.Agents)
	}
	if len(got.Commands) != 1 || got.Commands[0] != "run" {
		t.Fatalf("command names mismatch: %+v", got.Commands)
	}
	if len(got.MCPServers) != 1 || got.MCPServers[0] != "srv1" {
		t.Fatalf("mcp server names mismatch: %+v", got.MCPServers)
	}
}

func TestMCPServersFor(t *testing.T) {
	p := &Plugin{
		Manifest: Manifest{
			Name: "demo",
			MCPServers: map[string]MCPServerConfig{
				"srv1": {Command: "node", Args: []string{"s.js"}, DangerLevel: "high"},
				"srv2": {Command: "python", Args: []string{"-m", "srv"}},
				"srv3": {Command: "${CLAUDE_PLUGIN_ROOT}/mcp/neko-devkit-mcp", Args: []string{"${PLUGIN_ROOT}"}},
			},
		},
		Dir:     "/plugins/demo",
		Enabled: true,
	}
	got := MCPServersFor(p)
	if len(got) != 3 {
		t.Fatalf("expected 3 mcp views, got %d", len(got))
	}
	byName := map[string]common.MCPServerView{}
	for _, s := range got {
		byName[s.Name] = s
	}
	s1, ok := byName["srv1"]
	if !ok {
		t.Fatalf("missing srv1")
	}
	if s1.Plugin != "demo" || s1.Command != "node" || s1.DangerLevel != "high" || !s1.PluginEnabled {
		t.Fatalf("srv1 mismatch: %+v", s1)
	}
	if !reflect.DeepEqual(s1.Args, []string{"s.js"}) {
		t.Fatalf("srv1 args mismatch: %+v", s1.Args)
	}
	s2 := byName["srv2"]
	if !reflect.DeepEqual(s2.Args, []string{"-m", "srv"}) {
		t.Fatalf("srv2 args mismatch: %+v", s2.Args)
	}
	s3 := byName["srv3"]
	if s3.Command != "/plugins/demo/mcp/neko-devkit-mcp" {
		t.Fatalf("srv3 command mismatch: %+v", s3.Command)
	}
	if !reflect.DeepEqual(s3.Args, []string{"/plugins/demo"}) {
		t.Fatalf("srv3 args mismatch: %+v", s3.Args)
	}
}

func TestMCPServersForEmpty(t *testing.T) {
	p := &Plugin{Manifest: Manifest{Name: "bare"}, Dir: "/p"}
	got := MCPServersFor(p)
	if len(got) != 0 {
		t.Fatalf("expected empty mcp, got %+v", got)
	}
}

func TestSkillNamesFromDirsEmptyFiltered(t *testing.T) {
	got := skillNamesFromDirs([]string{"", "/x/y", ""})
	if !reflect.DeepEqual(got, []string{"y"}) {
		t.Fatalf("expected empty dirs filtered, got %+v", got)
	}
}
