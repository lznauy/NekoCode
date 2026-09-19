package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/config"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/project"
	"nekocode/bot/prompt"
	"nekocode/bot/provider/types"
	"nekocode/bot/session"
)

func writeWorkspaceFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestProjectMCPDoesNotContaminateGlobalConfiguration(t *testing.T) {
	t.Chdir(t.TempDir())
	b := newPersistTestBot(t)
	b.cfg.MCPServers = map[string]config.MCPServerConfig{
		"override":  {Command: "global", Args: []string{"global-arg"}, Env: map[string]string{"SECRET": "global"}, Enabled: true},
		"disabled":  {Command: "global", Enabled: true},
		"inherited": {Command: "global", Enabled: true},
	}
	writeWorkspaceFile(t, b.project.MCPPath(), `{"mcpServers":{"override":{"command":"project"},"disabled":{"enabled":false}}}`)
	if diagnostics := b.project.Refresh(); len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	servers := b.effectiveMCPServers()
	if len(servers) != 2 || servers["override"].Command != "project" || len(servers["override"].Args) != 0 || len(servers["override"].Env) != 0 || servers["inherited"].Command != "global" {
		t.Fatalf("bad effective config: %+v", servers)
	}
	if err := b.SwitchModel("alt"); err != nil {
		t.Fatal(err)
	}
	saved, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if saved.MCPServers["override"].Command != "global" || !saved.MCPServers["disabled"].Enabled {
		t.Fatalf("project leaked into global: %+v", saved.MCPServers)
	}
	view := b.Extensions()
	for _, srv := range view.ConfiguredMCP {
		if srv.Name == "override" && (srv.Source != b.project.MCPPath() || srv.Command != "project") {
			t.Fatalf("bad project source: %+v", srv)
		}
		if srv.Name == "disabled" && srv.Enabled {
			t.Fatal("disabled project override shown enabled")
		}
	}
}

func TestWorkspaceStartupReloadAndSessionRestore(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	path := filepath.Join(root, "NEKOCODE.md")
	writeWorkspaceFile(t, path, "Use project convention alpha.")
	writeWorkspaceFile(t, filepath.Join(root, ".nekocode", "skills", "local", "SKILL.md"), "---\nname: workspace-local\ndescription: local\n---\nlocal instructions")
	b := newPersistTestBot(t)
	if !strings.Contains(b.ctxMgr.Snapshot().SystemPrompt, "convention alpha") {
		t.Fatal("startup did not load rules")
	}
	if _, ok := b.ext.Skill("workspace-local"); !ok {
		t.Fatal("startup did not load project skill")
	}
	b.ctxMgr.Add("user", "keep this conversation")
	if err := b.saveSession(); err != nil {
		t.Fatal(err)
	}
	id := b.CurrentSessionID()
	writeWorkspaceFile(t, path, "Use project convention beta.")
	if err := b.ResumeSession(id); err != nil {
		t.Fatal(err)
	}
	if got := b.ctxMgr.Snapshot().SystemPrompt; !strings.Contains(got, "convention beta") || strings.Contains(got, "convention alpha") {
		t.Fatalf("stale restored rules: %s", got)
	}
	writeWorkspaceFile(t, path, "Use project convention gamma.")
	p := b.cmd.Parser()
	output, handled := p.Execute(context.Background(), p.Parse("/workspace reload"))
	if !handled || !strings.Contains(output, root) || !strings.Contains(b.ctxMgr.Snapshot().SystemPrompt, "convention gamma") {
		t.Fatalf("reload failed: %s", output)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	b.RefreshExtensions()
	if strings.Contains(b.ctxMgr.Snapshot().SystemPrompt, "convention gamma") {
		t.Fatal("deleted instructions retained")
	}
}

func TestProjectRulesSurviveCompaction(t *testing.T) {
	builder := prompt.New(t.TempDir())
	builder.SetProjectInstructions("NEKOCODE.md", "unique-project-rule")
	m := ctxmgr.New(ctxmgr.Config{SystemPrompt: builder.BuildStatic(), Summarizer: func([]types.Message, string) (string, error) { return "summary of earlier work", nil }})
	for range 8 {
		m.Add("user", "old question")
		m.Add("assistant", "old answer")
	}
	if ok, err := m.Summarize(context.Background()); err != nil || !ok {
		t.Fatalf("compaction: %v %v", ok, err)
	}
	if !strings.Contains(m.Build()[0].Content, "unique-project-rule") {
		t.Fatal("project rules lost during compaction")
	}
}

func TestCrossProjectSessionResumeRejectedBeforeMutation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	other := session.New(t.TempDir())
	saved := other.Current()
	saved.Messages = []types.Message{{Role: "user", Content: "other project history"}}
	if err := other.Save(saved); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	p, _ := project.New(root)
	b := &Bot{cwd: root, project: p, ctxMgr: ctxmgr.New(ctxmgr.Config{})}
	b.initSession()
	before := b.CurrentSessionID()
	if err := b.ResumeSession(saved.ID); err == nil || !strings.Contains(err.Error(), "session belongs to") {
		t.Fatalf("resume: %v", err)
	}
	if b.CurrentSessionID() != before || len(b.ctxMgr.Snapshot().Transcript) != 0 {
		t.Fatal("rejected resume mutated current session")
	}
}

func TestProjectDirectoryAliases(t *testing.T) {
	root := t.TempDir()
	if !sameProjectDirectory(root, filepath.Join(root, ".")) || sameProjectDirectory(root, t.TempDir()) {
		t.Fatal("incorrect project directory identity")
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if !sameProjectDirectory(root, alias) {
		t.Fatal("symlink to the same project rejected")
	}
}

func TestProjectRefreshConcurrentWithExtensionViews(t *testing.T) {
	t.Chdir(t.TempDir())
	b := newPersistTestBot(t)
	writeWorkspaceFile(t, b.project.MCPPath(), `{"mcpServers":{"local":{"enabled":false}}}`)
	b.ctxMgr.Add("user", "saved conversation")
	if err := b.saveSession(); err != nil {
		t.Fatal(err)
	}
	id := b.CurrentSessionID()
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				b.Extensions()
				b.CommandMenu(context.Background(), "$")
				b.ExecuteLocalCommand(context.Background(), "/help")
			}
		}
	}()
	defer func() { close(stop); <-done }()
	for range 20 {
		if _, err := b.NewSession(); err != nil {
			t.Fatal(err)
		}
		if err := b.ResumeSession(id); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSessionWithoutCWDHasExplicitDiagnostic(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	manager := session.New("")
	saved := manager.Current()
	saved.Messages = []types.Message{{Role: "user", Content: "history with missing metadata"}}
	if err := manager.Save(saved); err != nil {
		t.Fatal(err)
	}
	b := &Bot{cwd: t.TempDir(), ctxMgr: ctxmgr.New(ctxmgr.Config{})}
	b.initSession()
	before := b.CurrentSessionID()
	if err := b.ResumeSession(saved.ID); err == nil || !strings.Contains(err.Error(), "missing cwd") {
		t.Fatalf("missing cwd diagnostic: %v", err)
	}
	if b.CurrentSessionID() != before {
		t.Fatal("invalid session mutated active identity")
	}
}
