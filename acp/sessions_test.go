package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	controlruntime "nekocode/runtime"
)

func TestSessionLifecycle(t *testing.T) {
	backend := &fakeBackend{}
	s := &server{backend: backend, cwd: "/workspace", active: make(map[string]*activeTurn), conn: newConnection(io.Discard)}
	result, rpcErr := s.newSession(context.Background(), json.RawMessage(`{"cwd":"/workspace","mcpServers":[]}`))
	if rpcErr != nil || result.(map[string]any)["sessionId"] != "session-1" {
		t.Fatalf("new session = %#v, error = %#v", result, rpcErr)
	}
	if _, rpcErr := s.loadSession(context.Background(), json.RawMessage(`{"sessionId":"session-1","cwd":"/workspace","mcpServers":[]}`)); rpcErr != nil {
		t.Fatal(rpcErr)
	}
	if _, rpcErr := s.deleteSession(context.Background(), json.RawMessage(`{"sessionId":"session-1"}`)); rpcErr != nil || backend.deleted != "session-1" {
		t.Fatalf("delete error = %#v, deleted = %q", rpcErr, backend.deleted)
	}
	backend.sessions = []controlruntime.SessionMeta{{ID: "foreign", CWD: "/other"}}
	if _, rpcErr := s.deleteSession(context.Background(), json.RawMessage(`{"sessionId":"foreign"}`)); rpcErr == nil {
		t.Fatal("cross-workspace session deletion was accepted")
	}
}

func TestStdioMCPServers(t *testing.T) {
	backend := &fakeBackend{}
	s := &server{backend: backend, cwd: "/workspace", active: make(map[string]*activeTurn), conn: newConnection(io.Discard)}
	s.allowClientMCP = true
	request := `{"cwd":"/workspace","mcpServers":[` +
		`{"name":"fs","command":"/usr/bin/mcp-fs","args":["--root","/tmp"],"env":[{"name":"MODE","value":"ro"}]},` +
		`{"type":"stdio","name":"git","command":"/usr/bin/mcp-git"}` +
		`]}`
	if _, rpcErr := s.newSession(context.Background(), json.RawMessage(request)); rpcErr != nil {
		t.Fatal(rpcErr)
	}
	if len(backend.mcpAdded) != 2 || backend.mcpAdded[0] != "acp:fs" || backend.mcpAdded[1] != "acp:git" {
		t.Fatalf("registered servers = %#v", backend.mcpAdded)
	}
	if backend.mcpCleared != 0 {
		t.Fatalf("non-empty replacement was counted as a clear: %d", backend.mcpCleared)
	}

	// http/sse stay behind mcpCapabilities, which this agent leaves off.
	for _, serverType := range []string{"http", "sse"} {
		bad := `{"cwd":"/workspace","mcpServers":[{"type":"` + serverType + `","name":"x","url":"https://mcp.example.com"}]}`
		if _, rpcErr := s.newSession(context.Background(), json.RawMessage(bad)); rpcErr == nil {
			t.Fatalf("%s MCP server was accepted", serverType)
		}
	}
	// stdio without a command is malformed.
	if _, rpcErr := s.newSession(context.Background(), json.RawMessage(`{"cwd":"/workspace","mcpServers":[{"name":"broken"}]}`)); rpcErr == nil {
		t.Fatal("stdio server without command was accepted")
	}
	// A new session without servers releases the previously registered ones.
	if _, rpcErr := s.newSession(context.Background(), json.RawMessage(`{"cwd":"/workspace","mcpServers":[]}`)); rpcErr != nil {
		t.Fatal(rpcErr)
	}
	if backend.mcpCleared != 1 {
		t.Fatalf("servers were not cleared on replacement: %d", backend.mcpCleared)
	}
}

func TestClientMCPRequiresExplicitOptIn(t *testing.T) {
	backend := &fakeBackend{current: "old"}
	s := &server{backend: backend, cwd: "/workspace", active: make(map[string]*activeTurn), conn: newConnection(io.Discard)}
	params := json.RawMessage(`{"cwd":"/workspace","mcpServers":[{"name":"shell","command":"/bin/sh"}]}`)
	if _, rpcErr := s.newSession(context.Background(), params); rpcErr == nil {
		t.Fatal("client MCP was accepted without explicit opt-in")
	}
	if backend.current != "old" || len(backend.mcpAdded) != 0 {
		t.Fatalf("rejected MCP request changed backend: current=%q added=%v", backend.current, backend.mcpAdded)
	}
}

func TestMCPServerValidationLimits(t *testing.T) {
	duplicate := []mcpServerRef{
		{Name: "same", Command: "/bin/one"},
		{Name: "same", Command: "/bin/two"},
	}
	if rpcErr := validateMCPServers(duplicate); rpcErr == nil {
		t.Fatal("duplicate MCP names were accepted")
	}
	tooMany := make([]mcpServerRef, maxMCPServers+1)
	for i := range tooMany {
		tooMany[i] = mcpServerRef{Name: fmt.Sprintf("server-%d", i), Command: "/bin/true"}
	}
	if rpcErr := validateMCPServers(tooMany); rpcErr == nil {
		t.Fatal("oversized MCP list was accepted")
	}
}

// MCP child processes started during session setup must be released again
// when a later step of that setup fails, instead of lingering until the
// next session operation.
func TestSessionSetupFailureReleasesMCPServers(t *testing.T) {
	mcpParams := `{"cwd":"/workspace","mcpServers":[{"name":"echo","command":"/usr/bin/mcp-echo"}]}`

	backend := &fakeBackend{newSessionErr: errors.New("boom")}
	s := &server{backend: backend, cwd: "/workspace", active: make(map[string]*activeTurn), conn: newConnection(io.Discard)}
	s.allowClientMCP = true
	if _, rpcErr := s.newSession(context.Background(), json.RawMessage(mcpParams)); rpcErr == nil {
		t.Fatal("failed newSession was accepted")
	}
	if len(backend.mcpAdded) != 0 {
		t.Fatalf("mcpAdded = %v", backend.mcpAdded)
	}
	if backend.mcpCleared != 0 {
		t.Fatalf("MCP state changed before failed newSession: %d", backend.mcpCleared)
	}

	backend = &fakeBackend{resumeErr: errors.New("boom")}
	backend.sessions = []controlruntime.SessionMeta{{ID: "session-1", CWD: "/workspace"}}
	s = &server{backend: backend, cwd: "/workspace", active: make(map[string]*activeTurn), conn: newConnection(io.Discard)}
	s.allowClientMCP = true
	loadParams := `{"sessionId":"session-1","cwd":"/workspace","mcpServers":[{"name":"echo","command":"/usr/bin/mcp-echo"}]}`
	if _, rpcErr := s.loadSession(context.Background(), json.RawMessage(loadParams)); rpcErr == nil {
		t.Fatal("failed loadSession was accepted")
	}
	if len(backend.mcpAdded) != 0 {
		t.Fatalf("mcpAdded = %v", backend.mcpAdded)
	}
	if backend.mcpCleared != 0 {
		t.Fatalf("MCP state changed before failed loadSession: %d", backend.mcpCleared)
	}
}

func TestSessionSetupRollsBackOnMCPReplacementFailure(t *testing.T) {
	backend := &fakeBackend{
		current: "old", replaceMCPErr: errors.New("MCP unavailable"),
		models: []controlruntime.ModelOption{{Name: "default"}}, activeModel: "default",
	}
	s := &server{
		backend: backend, cwd: "/workspace", active: make(map[string]*activeTurn),
		conn: newConnection(io.Discard), allowClientMCP: true,
		defaultConfig: sessionConfig{Model: "default"},
	}
	params := json.RawMessage(`{"cwd":"/workspace","mcpServers":[{"name":"echo","command":"/usr/bin/mcp-echo"}]}`)
	if _, rpcErr := s.newSession(context.Background(), params); rpcErr == nil {
		t.Fatal("session setup succeeded despite MCP replacement failure")
	}
	if backend.current != "old" {
		t.Fatalf("current session = %q, want rollback to old", backend.current)
	}
}

// A session operation that fails its guards before applyMCPServers must not
// tear down MCP servers still owned by another live session (e.g. an active
// prompt or the currently loaded session).
func TestGuardFailureKeepsExistingMCPServers(t *testing.T) {
	// newSession rejected because a prompt is running on another session.
	backend := &fakeBackend{}
	s := &server{backend: backend, cwd: "/workspace", active: map[string]*activeTurn{
		"session-1": {runID: "run-1"},
	}, conn: newConnection(io.Discard)}
	s.allowClientMCP = true
	if _, rpcErr := s.newSession(context.Background(), json.RawMessage(`{"cwd":"/workspace","mcpServers":[{"name":"echo","command":"/usr/bin/mcp-echo"}]}`)); rpcErr == nil {
		t.Fatal("newSession with an active turn was accepted")
	}
	if backend.mcpCleared != 0 {
		t.Fatalf("newSession guard failure cleared MCP servers of a live session: %d", backend.mcpCleared)
	}

	// loadSession rejected because the requested session is unknown, while
	// another workspace session is currently loaded and owns its servers.
	backend = &fakeBackend{}
	backend.sessions = []controlruntime.SessionMeta{{ID: "session-1", CWD: "/workspace"}}
	s = &server{backend: backend, cwd: "/workspace", active: make(map[string]*activeTurn), conn: newConnection(io.Discard)}
	if _, rpcErr := s.loadSession(context.Background(), json.RawMessage(`{"sessionId":"ghost","cwd":"/workspace","mcpServers":[{"name":"echo","command":"/usr/bin/mcp-echo"}]}`)); rpcErr == nil {
		t.Fatal("loadSession of an unknown session was accepted")
	}
	if backend.mcpCleared != 0 {
		t.Fatalf("loadSession guard failure cleared MCP servers of the loaded session: %d", backend.mcpCleared)
	}
}

func TestListSessionsAcceptsMissingParams(t *testing.T) {
	backend := &fakeBackend{}
	s := &server{backend: backend, cwd: "/workspace", active: make(map[string]*activeTurn), conn: newConnection(io.Discard)}
	for _, params := range []json.RawMessage{nil, json.RawMessage("null"), json.RawMessage(`{}`), json.RawMessage(`{"cwd":"/workspace"}`)} {
		result, rpcErr := s.listSessions(params)
		if rpcErr != nil {
			t.Fatalf("listSessions(%s) error = %#v", params, rpcErr)
		}
		if _, ok := result.(map[string]any)["sessions"]; !ok {
			t.Fatalf("listSessions(%s) = %#v", params, result)
		}
	}
	if _, rpcErr := s.listSessions(json.RawMessage(`{"cursor":"nope"}`)); rpcErr == nil {
		t.Fatal("unknown cursor was accepted")
	}
}

func TestLoadSessionReportsUsage(t *testing.T) {
	var output bytes.Buffer
	backend := &fakeBackend{
		metrics: controlruntime.MetricsSnapshot{ContextTokens: 1200, ContextBudget: 64000},
	}
	backend.current = "session-1"
	backend.sessions = []controlruntime.SessionMeta{{ID: "session-1", CWD: "/workspace"}}
	s := &server{backend: backend, cwd: "/workspace", active: make(map[string]*activeTurn), conn: newConnection(&output)}
	if _, rpcErr := s.loadSession(context.Background(), json.RawMessage(`{"sessionId":"session-1","cwd":"/workspace","mcpServers":[]}`)); rpcErr != nil {
		t.Fatal(rpcErr)
	}
	if !strings.Contains(output.String(), `"usage_update"`) || !strings.Contains(output.String(), `"used":1200`) {
		t.Fatalf("load did not report usage: %s", output.String())
	}
}

func TestHistoryUpdatesReplayFullHistory(t *testing.T) {
	imagePath := filepath.Join(t.TempDir(), "chart.png")
	if err := os.WriteFile(imagePath, []byte("fakepng"), 0o600); err != nil {
		t.Fatal(err)
	}
	updates := historyUpdates(t.Context(), 3, controlruntime.DisplayMessage{
		Role:      "assistant",
		Content:   "done",
		Reasoning: "thinking",
		Blocks: []controlruntime.DisplayBlock{
			{ToolName: "shell", Args: `{"command":"ls"}`, Content: "listing", IsError: true},
		},
		Images: []controlruntime.ImageRef{{Path: imagePath, URL: "https://example.com/chart.png"}},
	})
	kinds := make([]string, 0, len(updates))
	for _, update := range updates {
		kinds = append(kinds, update["sessionUpdate"].(string))
	}
	want := []string{
		"agent_thought_chunk", "tool_call", "tool_call_update",
		"agent_message_chunk", "agent_message_chunk",
	}
	if len(kinds) != len(want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kinds[%d] = %q, want %q", i, kinds[i], want[i])
		}
	}
	if updates[1]["status"] != "failed" || updates[1]["toolCallId"] != "history-3-tool-0" {
		t.Fatalf("replayed tool_call = %#v", updates[1])
	}
	if updates[2]["status"] != "failed" || updates[2]["toolCallId"] != "history-3-tool-0" {
		t.Fatalf("replayed tool_call_update = %#v", updates[2])
	}
	image := updates[4]["content"].(map[string]any)
	if image["type"] != "image" || image["mimeType"] != "image/png" || image["uri"] != "https://example.com/chart.png" {
		t.Fatalf("replayed image = %#v", image)
	}

	user := historyUpdates(t.Context(), 0, controlruntime.DisplayMessage{Role: "user", Content: "hello"})
	if len(user) != 1 || user[0]["sessionUpdate"] != "user_message_chunk" {
		t.Fatalf("user replay = %#v", user)
	}

	var dropped []map[string]any
	dropped = historyUpdates(t.Context(), 1, controlruntime.DisplayMessage{Role: "system", Content: "note"})
	dropped = append(dropped, historyUpdates(t.Context(), 1, controlruntime.DisplayMessage{Role: "user", Content: ""})...)
	dropped = append(dropped, historyUpdates(t.Context(), 1, controlruntime.DisplayMessage{
		Role:   "assistant",
		Images: []controlruntime.ImageRef{{Path: filepath.Join(t.TempDir(), "missing.png")}},
	})...)
	if len(dropped) != 0 {
		t.Fatalf("expected no updates for empty/system history, got %#v", dropped)
	}
}

func TestReplayImageRespectsSizeAndContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxReplayImageBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	ref := controlruntime.ImageRef{Path: path}
	if got := imageContent(t.Context(), ref); got != nil {
		t.Fatal("oversize image replayed")
	}
	if err := os.WriteFile(path, []byte("image"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got := imageContent(ctx, ref); got != nil {
		t.Fatal("cancelled image replay continued")
	}
}
