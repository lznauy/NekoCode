package extension

import (
	"nekocode/bot/command"
	ctxmgr "nekocode/bot/contextmgr"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManualSkillsAndVisibleLoadErrors(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("HOME", filepath.Join(root, "home"))
	dir := filepath.Join(root, ".nekocode", "skills")
	for name, fields := range map[string]string{"manual": "disable-model-invocation: true", "broken": "context: fork", `bad name`: ""} {
		sd := filepath.Join(dir, name)
		if err := os.MkdirAll(sd, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sd, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: demo\n"+fields+"\n---\nworkflow"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m := newAgentManager()
	defer m.Close()
	m.Load()
	if _, ok := m.Skill("manual"); !ok {
		t.Fatal("manual selection unavailable")
	}
	found := false
	for _, cmd := range m.CommandSkills() {
		if cmd.Name == "manual" {
			if _, ok := cmd.Load(); !ok {
				t.Fatal("manual command failed")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("manual command missing")
	}
	if _, err := m.ModelSkill("manual"); err == nil {
		t.Fatal("automatic invocation allowed")
	}
	path := filepath.Join(dir, "broken", "SKILL.md")
	if len(m.Snapshot().SkillErrors) != 2 {
		t.Fatalf("snapshot errors: %v", m.Snapshot().SkillErrors)
	}
	contextManager := ctxmgr.New(ctxmgr.Config{})
	handler := command.New(command.Deps{CtxMgr: contextManager, ToolRegistry: m.tools})
	m.RegisterCommands(handler, nil)
	if registered, duringTask := handler.Parser().CommandAvailability("/skills"); !registered || !duringTask {
		t.Fatal("read-only /skills command unavailable")
	}
	report, handled := handler.Execute(t.Context(), "/skills", contextManager)
	if !handled {
		t.Fatal("/skills not handled")
	}
	for _, want := range []string{"$manual (manual-only)", path, "unsupported context", filepath.Join(dir, "bad name", "SKILL.md"), "$ command identifier", "whitespace"} {
		if !strings.Contains(report, want) {
			t.Fatalf("report missing %q: %s", want, report)
		}
	}
}
