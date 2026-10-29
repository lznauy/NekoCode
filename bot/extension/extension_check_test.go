package extension

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"nekocode/bot/command"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/plugin"
)

func checkFixture(t *testing.T, manifest string, files map[string]string) *plugin.Plugin {
	t.Helper()
	root := t.TempDir()
	files[".claude-plugin/plugin.json"] = manifest
	for path, body := range files {
		target := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return &plugin.Plugin{Dir: root, Manifest: plugin.Manifest{Name: "demo"}}
}
func checkText(result PluginCheck) string {
	var b strings.Builder
	for _, d := range result.Diagnostics {
		b.WriteString(d.Level + " " + d.Path + " " + d.Message + "\n")
	}
	return b.String()
}
func TestPluginCheckThreeShapesWithoutExecuting(t *testing.T) {
	for _, tc := range []struct {
		name, manifest string
		files          map[string]string
		want           []string
	}{
		{"skill-only", `{"name":"demo","skills":["skills"]}`, map[string]string{"skills/review/SKILL.md": "---\nname: review\ndescription: Review code\ndisable-model-invocation: true\n---\nReview changes."}, []string{"skill parses successfully (manual-only)"}},
		{"agent-hook", `{"name":"demo","agents":["agents/review.md"],"hooks":{"source":"hooks.json"}}`, map[string]string{"agents/review.md": "---\nname: reviewer\ntools: [Read]\n---\nReview.", "hooks.json": `{"SessionStart":[{"hooks":[{"type":"command","command":"exit 79"}]}]}`}, []string{"agent profile parses successfully", "hook configuration parses successfully"}},
		{"mcp", `{"name":"demo"}`, map[string]string{".mcp.json": `{"mcpServers":{"demo":{"command":"/must-not-be-executed","env":{"TOKEN":"secret-token"}}}}`}, []string{"MCP definition parses successfully"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := checkFixture(t, tc.manifest, tc.files)
			result := checkPlugin(t.Context(), p, func(string) bool { return true })
			text := checkText(result)
			if strings.Contains(text, "error ") {
				t.Fatal(text)
			}
			for _, want := range tc.want {
				if !strings.Contains(text, want) {
					t.Fatalf("missing %q: %s", want, text)
				}
			}
			if strings.Contains(text, "secret-token") || strings.Contains(text, "/must-not-be-executed") {
				t.Fatal("echoed configuration secrets", text)
			}
		})
	}
}
func TestPluginCheckReportsUnsupportedAndBrokenDefinitions(t *testing.T) {
	p := checkFixture(t, `{"name":"demo","outputStyles":"styles","skills":["skills","missing"],"agents":["agents/bad.md","../outside.md"],"commands":[{"name":"old","source":"commands/old.md"}],"hooks":{"source":"hooks.json"},"mcpServers":{"broken":{"env":{"SECRET":"hidden"}},"remote":{"url":"https://user:password@example.invalid","type":"http"}}}`, map[string]string{
		"skills/bad/SKILL.md": "---\nname: broken\ndescription: test\ncontext: fork\n---\nWorkflow.",
		"skills/ok/SKILL.md":  "---\nname: ok\ndescription: test\nuser-invocable: false\n---\nWorkflow.",
		"agents/bad.md":       "---\nname: bad\npermissionMode: full\n---\nReview.",
		"hooks.json":          `{"UnknownEvent":[]}`,
		"install.sh":          "exit 88\n",
	})
	text := checkText(checkPlugin(t.Context(), p, func(string) bool { return true }))
	for _, want := range []string{"unsupported manifest field: outputStyles", "commands declarations are not registered", "unsupported context", "ignored skill field: user-invocable", "permissionMode", "outside plugin root", "UnknownEvent", "requires a command or URL", "ignored MCP field in server remote: type", "missing", "installation script is not executed"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q: %s", want, text)
		}
	}
	for _, secret := range []string{"hidden", "password"} {
		if strings.Contains(text, secret) {
			t.Fatal("leaked secret: ", text)
		}
	}
}
func TestPluginCheckRejectsEscapingSymlinkAndMalformedMCP(t *testing.T) {
	p := checkFixture(t, `{"name":"demo","skills":["skills"]}`, map[string]string{".mcp.json": "{broken"})
	target := filepath.Join(t.TempDir(), "SKILL.md")
	if err := os.WriteFile(target, []byte("secret-content"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(p.Dir, "skills"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(p.Dir, "skills", "SKILL.md")); err != nil {
		t.Skip(err)
	}
	text := checkText(checkPlugin(t.Context(), p, nil))
	if !strings.Contains(text, "error skills/SKILL.md") || !strings.Contains(text, "error .mcp.json") {
		t.Fatal(text)
	}
	if strings.Contains(text, "secret-content") {
		t.Fatal("read external content")
	}
}
func TestPluginCheckHooksRejectEscapingSymlinks(t *testing.T) {
	for _, kind := range []string{"config", "external JS"} {
		t.Run(kind, func(t *testing.T) {
			p := checkFixture(t, `{"name":"demo","hooks":{"source":"hooks.json"}}`, map[string]string{})
			outside := filepath.Join(t.TempDir(), "outside")
			body := `{"PreModel":[]}`
			link := filepath.Join(p.Dir, "hooks.json")
			if kind == "external JS" {
				body = `null`
				link = filepath.Join(p.Dir, "hook.js")
				config := `{"PreModel":[{"hooks":[{"type":"js","path":"hook.js"}]}]}`
				if err := os.WriteFile(filepath.Join(p.Dir, "hooks.json"), []byte(config), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(outside, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, link); err != nil {
				t.Skip(err)
			}
			text := checkText(checkPlugin(t.Context(), p, nil))
			if !strings.Contains(text, "error hooks.json") || strings.Contains(text, "ok hooks.json") {
				t.Fatalf("escaping hook symlink accepted: %s", text)
			}
		})
	}
}

func TestPluginCheckCommandPreservesDisabledStateAndRegistry(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("HOME", t.TempDir())
	dir := agentFixture(t, root, "demo plugin", `"agents/review.md"`)
	m := newAgentManager()
	defer m.Close()
	m.Load()
	if _, err := m.plugins.SetEnabled("demo plugin", false); err != nil {
		t.Fatal(err)
	}
	// Disabled definitions must be inspected without activation or reload.
	if err := os.WriteFile(filepath.Join(dir, "agents", "review.md"), []byte("---\nname: reviewer\npermissionMode: full\n---\nReview."), 0600); err != nil {
		t.Fatal(err)
	}
	before := m.Snapshot()
	cm := ctxmgr.New(ctxmgr.Config{})
	handler := command.New(command.Deps{CtxMgr: cm})
	m.RegisterCommands(handler, nil)
	report, handled := handler.Execute(t.Context(), "/plugin check demo plugin", cm)
	if !handled || !strings.Contains(report, "enabled=false") || !strings.Contains(report, "permissionMode") {
		t.Fatal(report)
	}
	after := m.Snapshot()
	if !reflect.DeepEqual(before.Plugins, after.Plugins) || !reflect.DeepEqual(before.Agents, after.Agents) || !reflect.DeepEqual(before.MCPHealth, after.MCPHealth) || !reflect.DeepEqual(before.LoadedSkills, after.LoadedSkills) {
		t.Fatal("check mutated runtime state")
	}
	if _, err := m.CheckPlugins(t.Context(), "missing"); err == nil {
		t.Fatal("unknown plugin accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := m.CheckPlugins(ctx, ""); err != context.Canceled {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestPluginCheckMatchesRuntimeSkillDiscovery(t *testing.T) {
	p := checkFixture(t, `{"name":"demo","skills":["skills"]}`, map[string]string{
		"skills/review/SKILL.md":           "---\nname: review\ndescription: Review\n---\nWorkflow.",
		"skills/review/reference/SKILL.md": "not a skill; documentation example",
	})
	text := checkText(checkPlugin(t.Context(), p, nil))
	if strings.Contains(text, "reference/") || strings.Contains(text, "error ") {
		t.Fatal(text)
	}
}
func TestPluginCheckUsesRuntimeMCPURLRules(t *testing.T) {
	p := checkFixture(t, `{"name":"demo","mcpServers":{"both":{"command":"server","url":"https://example.invalid"},"plain":{"url":"http://example.invalid"},"credentials":{"url":"https://user:secret@example.invalid"},"fragment":{"url":"https://example.invalid/#secret"},"local":{"url":"http://127.0.0.1:1234"}}}`, map[string]string{})
	text := checkText(checkPlugin(t.Context(), p, nil))
	for _, name := range []string{"both", "plain", "credentials", "fragment"} {
		if !strings.Contains(text, "error mcpServers."+name) || strings.Contains(text, "ok mcpServers."+name) {
			t.Errorf("invalid config accepted %s: %s", name, text)
		}
	}
	if !strings.Contains(text, "ok mcpServers.local") || strings.Contains(text, "secret") {
		t.Fatal(text)
	}
}
func TestPluginCheckDiscoveryFailureWithInlineMCP(t *testing.T) {
	p := checkFixture(t, `{"name":"demo","mcpServers":{"demo":{"command":"not-executed"}}}`, map[string]string{"hidden/skills/review/SKILL.md": "secret"})
	hidden := filepath.Join(p.Dir, "hidden")
	if err := os.Chmod(hidden, 0000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(hidden, 0700)
	if _, err := os.ReadDir(hidden); err == nil {
		t.Skip("directory permissions not enforced for this user")
	}
	text := checkText(checkPlugin(t.Context(), p, nil))
	if !strings.Contains(text, "component discovery incomplete") {
		t.Fatal(text)
	}
}

func TestPluginCheckRecognizesComponentNamedPluginRoot(t *testing.T) {
	for _, kind := range []string{"skills", "agents"} {
		t.Run(kind, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), kind)
			if e := os.MkdirAll(filepath.Join(root, ".claude-plugin"), 0700); e != nil {
				t.Fatal(e)
			}
			if e := os.WriteFile(filepath.Join(root, ".claude-plugin", "plugin.json"), []byte(`{"name":"`+kind+`"}`), 0600); e != nil {
				t.Fatal(e)
			}
			file, body := "SKILL.md", "---\nname: root\ndescription: Root skill\ncontext: fork\n---\nWorkflow."
			if kind == "agents" {
				file = "review.md"
				body = "---\nname: review\npermissionMode: full\n---\nReview."
			}
			if e := os.WriteFile(filepath.Join(root, file), []byte(body), 0600); e != nil {
				t.Fatal(e)
			}
			p := &plugin.Plugin{Dir: root, Manifest: plugin.Manifest{Name: kind}}
			text := checkText(checkPlugin(t.Context(), p, func(string) bool { return true }))
			if !strings.Contains(text, "error "+file) {
				t.Fatalf("root component not checked: %s", text)
			}
		})
	}
}
func TestPluginDiagnosticTruncationIsUTF8SafeAndVisible(t *testing.T) {
	checks := []PluginCheck{{Diagnostics: []PluginDiagnostic{{
		Level:   "warning",
		Path:    strings.Repeat("p", maxPluginDiagnosticPathBytes-1) + "界",
		Message: strings.Repeat("m", maxPluginDiagnosticMessageBytes-1) + "界",
	}}}}
	checks = limitPluginDiagnosticBytes(checks, maxPluginCheckDiagnosticBytes, false)
	if len(checks[0].Diagnostics) != 2 {
		t.Fatalf("diagnostics = %+v", checks[0].Diagnostics)
	}
	diagnostic := checks[0].Diagnostics[0]
	if len(diagnostic.Path) > maxPluginDiagnosticPathBytes || len(diagnostic.Message) > maxPluginDiagnosticMessageBytes {
		t.Fatal("diagnostic field exceeds its byte limit")
	}
	if !utf8.ValidString(diagnostic.Path) || !utf8.ValidString(diagnostic.Message) {
		t.Fatal("truncation produced invalid UTF-8")
	}
	assertIncompleteDiagnostic(t, checks)
	encoded, err := json.Marshal(checks)
	if err != nil || !json.Valid(encoded) {
		t.Fatalf("diagnostics are not JSON encodable: %v", err)
	}
}

func TestPluginDiagnosticIncompleteCountsTowardEntryLimit(t *testing.T) {
	check := PluginCheck{}
	for range maxPluginDiagnostics {
		check.Diagnostics = append(check.Diagnostics, PluginDiagnostic{Level: "warning", Path: "skill.md", Message: "warning"})
	}
	limited := limitPluginDiagnosticBytes([]PluginCheck{check}, maxPluginCheckDiagnosticBytes, true)
	if got := len(limited[0].Diagnostics); got != maxPluginDiagnostics {
		t.Fatalf("diagnostic count = %d, limit %d", got, maxPluginDiagnostics)
	}
	assertIncompleteDiagnostic(t, limited)
}

func TestPluginDiagnosticIncompleteSortsAfterAppend(t *testing.T) {
	checks := []PluginCheck{{Diagnostics: []PluginDiagnostic{
		{Level: "warning", Path: "z", Message: "警告"},
		{Level: "ok", Path: "a", Message: "完成"},
		{Level: "error", Path: "z", Message: "last"},
		{Level: "error", Path: "a", Message: "second"},
		{Level: "error", Path: "a", Message: "first"},
	}}}
	limited := limitPluginDiagnosticBytes(checks, maxPluginCheckDiagnosticBytes, true)
	want := []PluginDiagnostic{
		{Level: "error", Path: ".", Message: pluginReportIncompleteMessage},
		{Level: "error", Path: "a", Message: "first"},
		{Level: "error", Path: "a", Message: "second"},
		{Level: "error", Path: "z", Message: "last"},
		{Level: "warning", Path: "z", Message: "警告"},
		{Level: "ok", Path: "a", Message: "完成"},
	}
	if !reflect.DeepEqual(limited[0].Diagnostics, want) {
		t.Fatalf("diagnostics = %+v, want %+v", limited[0].Diagnostics, want)
	}
	if len(limited[0].Diagnostics) > maxPluginDiagnostics {
		t.Fatalf("diagnostic count = %d, limit %d", len(limited[0].Diagnostics), maxPluginDiagnostics)
	}
	encoded, err := json.Marshal(limited[0].Diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > maxPluginCheckDiagnosticBytes {
		t.Fatalf("diagnostics use %d bytes, limit %d", len(encoded), maxPluginCheckDiagnosticBytes)
	}
	for _, diagnostic := range limited[0].Diagnostics {
		if !utf8.ValidString(diagnostic.Path) || !utf8.ValidString(diagnostic.Message) {
			t.Fatalf("sorting changed UTF-8: %+v", diagnostic)
		}
	}
}

func TestPluginDiagnosticByteBudgets(t *testing.T) {
	plugin := PluginCheck{}
	for range maxPluginDiagnostics {
		plugin.Diagnostics = append(plugin.Diagnostics, PluginDiagnostic{
			Level: "warning", Path: "skill.md", Message: strings.Repeat("m", maxPluginDiagnosticMessageBytes),
		})
	}
	limitedPlugin := limitPluginDiagnosticBytes([]PluginCheck{plugin}, maxPluginCheckDiagnosticBytes, false)
	encoded, err := json.Marshal(limitedPlugin[0].Diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > maxPluginCheckDiagnosticBytes {
		t.Fatalf("plugin diagnostics use %d bytes, limit %d", len(encoded), maxPluginCheckDiagnosticBytes)
	}
	assertIncompleteDiagnostic(t, limitedPlugin)

	checks := make([]PluginCheck, 5)
	for i := range checks {
		checks[i].Name = string(rune(97 + i))
		for range 40 {
			checks[i].Diagnostics = append(checks[i].Diagnostics, PluginDiagnostic{
				Level: "warning", Path: "skill.md", Message: strings.Repeat("m", maxPluginDiagnosticMessageBytes),
			})
		}
	}
	checks = limitPluginDiagnosticBytes(checks, maxPluginCheckReportDiagnosticBytes, false)
	var flattened []PluginDiagnostic
	for _, check := range checks {
		flattened = append(flattened, check.Diagnostics...)
	}
	encoded, err = json.Marshal(flattened)
	if err != nil || !json.Valid(encoded) {
		t.Fatalf("report is not JSON encodable: %v", err)
	}
	if len(encoded) > maxPluginCheckReportDiagnosticBytes {
		t.Fatalf("report diagnostics use %d bytes, limit %d", len(encoded), maxPluginCheckReportDiagnosticBytes)
	}
	assertIncompleteDiagnostic(t, checks)
}

func assertIncompleteDiagnostic(t *testing.T, checks []PluginCheck) {
	t.Helper()
	count := 0
	for _, check := range checks {
		for _, diagnostic := range check.Diagnostics {
			if diagnostic.Level == "error" && diagnostic.Path == "." && diagnostic.Message == pluginReportIncompleteMessage {
				count++
			}
		}
	}
	if count != 1 {
		t.Fatalf("incomplete diagnostic count = %d, checks = %+v", count, checks)
	}
}

func TestPluginCheckMenuIncludesAllPlugins(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("HOME", t.TempDir())
	agentFixture(t, root, "demo", `"agents/review.md"`)
	m := newAgentManager()
	defer m.Close()
	m.Load()
	cm := ctxmgr.New(ctxmgr.Config{})
	handler := command.New(command.Deps{CtxMgr: cm})
	m.RegisterCommands(handler, nil)
	menu, ok := handler.Menu(t.Context(), "/plugin check")
	if !ok {
		t.Fatal("missing check menu")
	}
	found := false
	for _, item := range menu.Items {
		if item.Value == "/plugin check" && item.Submit {
			found = true
		}
	}
	if !found {
		t.Fatal("menu cannot execute all-plugin check")
	}
}

func TestPluginCheckReportPrioritizesSummaryAndProblems(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("HOME", t.TempDir())
	dir := filepath.Join(root, ".nekocode", "plugins", "demo")
	files := map[string]string{
		".claude-plugin/plugin.json": `{"name":"demo","skills":["skills"],"hooks":{"source":"z-hooks.json"}}`,
		"skills/review/SKILL.md":     "---\nname: review\ndescription: Review\n---\nWorkflow.",
		"z-hooks.json":               `{"UnknownEvent":[]}`,
	}
	for path, data := range files {
		target := filepath.Join(dir, path)
		if e := os.MkdirAll(filepath.Dir(target), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(target, []byte(data), 0600); e != nil {
			t.Fatal(e)
		}
	}
	m := newAgentManager()
	defer m.Close()
	m.Load()
	report := m.pluginCheckReport(t.Context(), []string{"demo"})
	summary, problem, passed := strings.Index(report, "Result:"), strings.Index(report, "[error]"), strings.Index(report, "[ok]")
	if summary < 0 || problem < 0 || passed < 0 || summary > problem || problem > passed {
		t.Fatalf("summary/problems buried below successful checks: %s", report)
	}
}
