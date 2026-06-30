package skill

import (
	"reflect"
	"testing"

	"nekocode/common"
)

func TestBuildManagementView(t *testing.T) {
	reg := NewRegistry()
	reg.RegisterBundled([]*Skill{
		{Name: "builtin", Description: "built in"},
		{Name: "plugin-skill", Description: "from plugin", Dir: "/plugins/p/skills/s1", Files: []string{"b", "a"}},
	})
	reg.MarkLoaded("builtin")

	plugins := []common.PluginView{{
		Name:   "p",
		Skills: []string{"/plugins/p/skills"},
	}}

	got := BuildManagementView(reg, plugins, nil)
	if len(got.Skills) != 2 || len(got.Plugins) != 1 {
		t.Fatalf("unexpected view: %+v", got)
	}
	if got.Skills[0].Name != "builtin" || !got.Skills[0].Loaded || got.Skills[0].Source != "内置" || got.Skills[0].SourceKind != "builtin" {
		t.Fatalf("builtin skill mismatch: %+v", got.Skills[0])
	}
	if got.Skills[1].Name != "plugin-skill" || got.Skills[1].Source != "插件" || got.Skills[1].SourceKind != "plugin" || got.Skills[1].Plugin != "p" {
		t.Fatalf("plugin skill mismatch: %+v", got.Skills[1])
	}
	if !reflect.DeepEqual(got.Skills[1].Files, []string{"a", "b"}) {
		t.Fatalf("files not sorted: %+v", got.Skills[1].Files)
	}
}

func TestSourceForDirKinds(t *testing.T) {
	plugins := []common.PluginView{{Name: "p", Skills: []string{"/plugins/p/skills"}}}
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
		kind, label, plug := SourceForDir(c.dir, plugins)
		if kind != c.wantKind || label != c.wantLabel || plug != c.wantPlug {
			t.Errorf("SourceForDir(%q) = (%q,%q,%q), want (%q,%q,%q)", c.dir, kind, label, plug, c.wantKind, c.wantLabel, c.wantPlug)
		}
	}
}
