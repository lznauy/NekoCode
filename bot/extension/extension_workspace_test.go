package extension

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nekocode/bot/extension/mcp"
)

// A real subprocess whose missing dependency can be repaired without changing
// its launch configuration, to exercise failed-start recovery on refresh.
func TestWorkspaceMCPProcess(t *testing.T) {
	dependency := os.Getenv("NEKOCODE_TEST_MCP_DEPENDENCY")
	if dependency == "" {
		return
	}
	if _, err := os.Stat(dependency); err != nil {
		os.Exit(1)
	}
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var request struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil || request.ID == nil {
			continue
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2024-11-05", "serverInfo": map[string]string{"name": "test", "version": "1"}}
		case "tools/list":
			result = map[string]any{"tools": []any{}}
		default:
			continue
		}
		if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result}); err != nil {
			os.Exit(2)
		}
	}
	os.Exit(0)
}

func waitWorkspaceMCPStatus(t *testing.T, m *Manager, name, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if m.mcp.Health()[name].Status == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("MCP %s never reached %s: %+v", name, want, m.mcp.Health())
}

func TestWorkspaceRefreshRetriesFailedMCPWithoutConfigChange(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := New(Config{ProjectRoot: t.TempDir()})
	defer m.Close()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dependency := filepath.Join(t.TempDir(), "dependency")
	cfg := mcp.ServerConfig{Command: executable, Args: []string{"-test.run=^TestWorkspaceMCPProcess$"}, Env: map[string]string{"NEKOCODE_TEST_MCP_DEPENDENCY": dependency}}
	if err := m.AddMCPServerBackground("recover", cfg); err != nil {
		t.Fatal(err)
	}
	waitWorkspaceMCPStatus(t, m, "recover", mcp.StatusError)
	if err := os.WriteFile(dependency, []byte("available"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.ReloadWithMCP(map[string]mcp.ServerConfig{"recover": cfg})
	waitWorkspaceMCPStatus(t, m, "recover", mcp.StatusReady)
}

func TestWorkspaceRefreshReconcilesHostMCP(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := New(Config{ProjectRoot: t.TempDir()})
	defer m.Close()
	missing := filepath.Join(t.TempDir(), "missing-executable")
	initial := mcp.ServerConfig{Command: missing, Args: []string{"initial"}}
	if err := m.AddMCPServerBackground("same", initial); err != nil {
		t.Fatal(err)
	}
	if err := m.mcp.AddBackground("session:acp:other", "other", mcp.ServerConfig{Command: missing}); err != nil {
		t.Fatal(err)
	}
	m.sessionMCP["acp"] = []string{"session:acp:other"}
	m.ReloadWithMCP(map[string]mcp.ServerConfig{"same": initial})
	if m.mcp.Owner("other") != "session:acp:other" {
		t.Fatal("refresh removed unrelated session server")
	}
	next := mcp.ServerConfig{Command: missing, Args: []string{"updated"}}
	m.ReloadWithMCP(map[string]mcp.ServerConfig{"same": next, "new": initial})
	if m.configMCP["same"].Args[0] != "updated" || m.mcp.Owner("new") != "config:new" {
		t.Fatal("refresh failed to update servers")
	}
	m.ReloadWithMCP(nil)
	if m.mcp.Owner("same") != "" || m.mcp.Owner("new") != "" || m.mcp.Owner("other") == "" {
		t.Fatal("refresh removed wrong ownership set")
	}
}

func TestWorkspaceRemovalRestoresShadowedSessionMCP(t *testing.T) {
	for _, resubmit := range []bool{false, true} {
		t.Run(fmt.Sprint(resubmit), func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			m := New(Config{ProjectRoot: t.TempDir()})
			defer m.Close()
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			dependency := filepath.Join(t.TempDir(), "available")
			if err := os.WriteFile(dependency, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			cfg := mcp.ServerConfig{Command: executable, Args: []string{"-test.run=^TestWorkspaceMCPProcess$"}, Env: map[string]string{"NEKOCODE_TEST_MCP_DEPENDENCY": dependency}}
			configs := map[string]mcp.ServerConfig{"same": cfg}
			if err := m.ReplaceSessionMCPServers(context.Background(), "acp", configs); err != nil {
				t.Fatal(err)
			}
			m.ReloadWithMCP(configs)
			if m.mcp.Owner("same") != "config:same" {
				t.Fatal("project override not active")
			}
			if resubmit {
				if err := m.ReplaceSessionMCPServers(context.Background(), "acp", configs); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Remove(dependency); err != nil {
				t.Fatal(err)
			}
			m.ReloadWithMCP(nil)
			if got := m.mcp.Owner("same"); got != "session:acp:same" {
				t.Fatalf("session server not restored: owner=%q", got)
			}
			waitWorkspaceMCPStatus(t, m, "same", mcp.StatusError)
			if err := os.WriteFile(dependency, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			m.ReloadWithMCP(nil)
			waitWorkspaceMCPStatus(t, m, "same", mcp.StatusReady)
			m.ReloadWithMCP(configs)
			if err := m.ReplaceSessionMCPServers(context.Background(), "acp", nil); err != nil {
				t.Fatal(err)
			}
			m.ReloadWithMCP(nil)
			if m.mcp.Owner("same") != "" {
				t.Fatal("explicitly removed session definition was resurrected")
			}
		})
	}
}

func TestExtensionsStayInConfiguredProject(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	pluginDir := filepath.Join(root, ".nekocode", "plugins", "local")
	if err := os.MkdirAll(filepath.Join(pluginDir, ".claude-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, ".claude-plugin", "plugin.json"), []byte(`{"name":"local"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	m := New(Config{ProjectRoot: root})
	defer m.Close()
	t.Chdir(t.TempDir())
	m.Load()
	m.Reload()
	snapshot := m.Snapshot()
	if len(snapshot.Plugins) != 1 || snapshot.Plugins[0].Dir != pluginDir {
		t.Fatalf("plugin discovery changed project: %+v", snapshot.Plugins)
	}
}
