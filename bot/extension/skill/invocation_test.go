package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManualOnlyInvocation(t *testing.T) {
	r := newRegistry()
	r.Register(&Skill{Name: "manual", Description: "secret description", Content: "manual instructions", DisableModelInvocation: true})
	r.Register(&Skill{Name: "normal", Description: "normal", Content: "normal instructions"})
	catalog := buildSkillListText(r.List(), 32000)
	if strings.Contains(catalog, "manual") || strings.Contains(catalog, "secret") || strings.Contains(catalog, "omitted") {
		t.Fatalf("manual skill leaked: %s", catalog)
	}
	tool := newSkillTool(r)
	names := tool.Parameters()[0].Enum
	if len(names) != 1 || names[0] != "normal" {
		t.Fatalf("model enum: %v", names)
	}
	if _, err := tool.Execute(t.Context(), map[string]any{"name": "manual"}); err == nil || !strings.Contains(err.Error(), "manual-only") {
		t.Fatalf("guessed invocation: %v", err)
	}
	if _, err := tool.Execute(t.Context(), map[string]any{"name": "normal"}); err != nil {
		t.Fatal(err)
	}
	if sk, ok := r.Get("manual"); !ok || !strings.Contains(FormatForContext(sk), "manual instructions") {
		t.Fatal("manual resolution lost")
	}
	if got := buildSkillListText([]*Skill{{Name: "hidden", DisableModelInvocation: true}}, 32000); got != "" {
		t.Fatalf("empty catalog: %q", got)
	}
}

func TestLoadDiagnosticsAndReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	write := func(fields string) {
		t.Helper()
		if err := os.WriteFile(path, []byte("---\nname: demo\ndescription: demo\n"+fields+"\n---\nbody"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("context: fork")
	m := NewWithDirs(nil, nil, 32000, []string{dir})
	m.Load(nil)
	errors := m.LoadErrors()
	if !strings.Contains(errors[path], "unsupported context") {
		t.Fatalf("diagnostic: %v", errors)
	}
	delete(errors, path)
	if len(m.LoadErrors()) != 1 {
		t.Fatal("diagnostics alias registry")
	}
	if _, ok := m.Get("demo"); ok {
		t.Fatal("invalid skill registered")
	}
	write("context: inline")
	m.Reload(nil)
	if len(m.LoadErrors()) != 0 {
		t.Fatalf("stale diagnostic: %v", m.LoadErrors())
	}
	if _, err := m.ForModel("demo"); err != nil {
		t.Fatal(err)
	}
}
