package standard

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"nekocode/bot/config"
	"nekocode/bot/core"
	"nekocode/bot/session"
	"nekocode/mode"
	controlruntime "nekocode/runtime"
)

func TestExecutorUsesCapturedMCPAndMediaWithInheritedPermissions(t *testing.T) {
	for _, deny := range []bool{false, true} {
		t.Run(fmt.Sprintf("deny_mcp_full=%v", deny), func(t *testing.T) {
			root := t.TempDir()
			t.Chdir(root)
			t.Setenv("HOME", filepath.Join(root, "home"))
			path := filepath.Join(root, "image.png")
			file, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(file, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
				t.Fatal(err)
			}
			file.Close()
			var mcpCalls, mediaCalls, workerCalls, parentCalls, approvals atomic.Int32
			var resumedID atomic.Value
			service := sdk.NewServer(&sdk.Implementation{Name: "fixture", Version: "1"}, nil)
			service.AddTool(&sdk.Tool{Name: "lookup", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
				mcpCalls.Add(1)
				return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "MCP_EXECUTOR_PROOF"}}}, nil
			})
			mcpServer := httptest.NewServer(sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return service }, &sdk.StreamableHTTPOptions{Stateless: true}))
			defer mcpServer.Close()
			reviewed := make(chan struct{}, 1)
			tool := func(index int, name string, args any) map[string]any {
				data, _ := json.Marshal(args)
				return map[string]any{"index": index, "id": fmt.Sprintf("%s-%d", name, index), "type": "function", "function": map[string]any{"name": name, "arguments": string(data)}}
			}
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Model    string          `json:"model"`
					Messages json.RawMessage `json:"messages"`
					Tools    []struct {
						Function struct {
							Name string `json:"name"`
						} `json:"function"`
					} `json:"tools"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					return
				}
				if request.Model == "vision" {
					mediaCalls.Add(1)
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"MEDIA_EXECUTOR_PROOF"}}]}`)
					return
				}
				child := false
				for _, entry := range request.Tools {
					if entry.Function.Name == "nekocode_submit_result" {
						child = true
					}
				}
				var delta map[string]any
				finish := "stop"
				if child {
					visible := map[string]bool{}
					for _, entry := range request.Tools {
						visible[entry.Function.Name] = true
					}
					for _, name := range []string{"capability", "image_understand", "shell", "write"} {
						if !visible[name] {
							t.Errorf("executor missing %s", name)
						}
					}
					if visible["task_submit"] || visible["task"] || visible["agent_profiles"] {
						t.Error("executor can delegate recursively")
					}
					if workerCalls.Add(1)%2 == 1 {
						delta = map[string]any{"tool_calls": []any{
							tool(0, "capability", map[string]any{"action": "call", "server": "demo", "tool": "lookup", "args": map[string]any{}}),
							tool(1, "image_understand", map[string]any{"path": path, "prompt": "Describe fixture"}),
							tool(2, "task_submit", map[string]any{"prompt": "recursive"}),
						}}
						finish = "tool_calls"
					} else {
						content := string(request.Messages)
						want := "MCP_EXECUTOR_PROOF"
						if deny {
							want = "denied"
						}
						if !strings.Contains(content, want) || !strings.Contains(content, "MEDIA_EXECUTOR_PROOF") || !strings.Contains(content, "disabled by sub-agent profile") {
							t.Errorf("worker outcomes missing expected MCP/media/profile evidence: %s", content)
						}
						delta = map[string]any{"tool_calls": []any{tool(0, "nekocode_submit_result", map[string]any{"summary": "executor verified", "evidence": []string{"MCP and media tool results"}, "files": []string{}, "verification": "Actual tool invocations", "unfinished": []string{}, "risks": []string{}})}}
						finish = "tool_calls"
					}
				} else {
					switch parentCalls.Add(1) {
					case 1:
						delta = map[string]any{"tool_calls": []any{tool(0, "task_submit", map[string]any{"required_tools": []string{"capability", "image_understand"}, "title": "service verification", "prompt": "Inspect configured MCP demo lookup and image " + path})}}
						finish = "tool_calls"
					case 2:
						delta = map[string]any{"content": "Executor dispatched"}
					case 3:
						content := string(request.Messages)
						_, tail, ok := strings.Cut(content, "[Subagent ID: ")
						if !ok {
							t.Error("missing resumable executor task ID")
							delta = map[string]any{"content": "Missing task"}
							break
						}
						id, _, _ := strings.Cut(tail, "]")
						resumedID.Store(id)
						delta = map[string]any{"tool_calls": []any{tool(0, "task_submit", map[string]any{"required_tools": []string{"capability", "image_understand"}, "resume_id": id, "prompt": "Recheck services under saved executor capabilities"})}}
						finish = "tool_calls"
					case 4:
						delta = map[string]any{"content": "Executor resumed"}
					default:
						reviewed <- struct{}{}
						delta = map[string]any{"content": "Executor review complete"}
					}
				}
				w.Header().Set("Content-Type", "text/event-stream")
				for _, choice := range []any{map[string]any{"index": 0, "delta": delta}, map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finish}} {
					data, _ := json.Marshal(map[string]any{"choices": []any{choice}})
					fmt.Fprintf(w, "data: %s\n\n", data)
				}
				fmt.Fprint(w, "data: [DONE]\n\n")
			}))
			defer model.Close()
			permissions := &config.PermissionsConfig{Ask: []string{"mcp__demo__lookup"}}
			if deny {
				permissions = &config.PermissionsConfig{Deny: []string{"mcp__demo__lookup"}}
			}
			if err := config.Save(config.Config{Active: "local/main", Providers: []config.ProviderConfig{{Name: "local", APIKey: "fixture", BaseURL: model.URL + "/v1", Protocol: "openai", Models: []config.ModelDefinition{{Name: "main", Model: "main"}}}}, MCPServers: map[string]config.MCPServerConfig{"demo": {URL: mcpServer.URL, Enabled: true}}, Permissions: permissions, ImageUnderstandModels: []config.ImageUnderstandConfig{{Name: "vision", Model: "vision", Provider: "openai", Protocol: "openai", APIKey: "fixture", BaseURL: model.URL + "/v1"}}}); err != nil {
				t.Fatal(err)
			}
			bot, err := core.New()
			if err != nil {
				t.Fatal(err)
			}
			assertFixtureModel(t, bot, "local/main", model.URL+"/v1")
			if err := bot.SetInteractionMode(mode.SuperAgent); err != nil {
				t.Fatal(err)
			}
			if deny {
				bot.SetFullAccess(true)
			}
			rt := FromBot(bot)
			defer rt.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			// MCP registration is asynchronous; gate the test on actual ready state.
			deadline := time.NewTimer(5 * time.Second)
			defer deadline.Stop()
			for bot.Extensions().MCPHealth["demo"].Status != "ready" {
				select {
				case <-time.After(time.Millisecond):
				case <-deadline.C:
					t.Fatalf("MCP did not become ready: health=%+v; %s", bot.Extensions().MCPHealth, runtimeTestDiagnostic(rt))
				}
			}
			events, err := rt.Events(ctx, controlruntime.EventFilter{})
			if err != nil {
				t.Fatal(err)
			}
			go func() {
				for event := range events {
					if event.Type == controlruntime.EventApprovalRequested {
						view := event.Payload.(controlruntime.ApprovalView)
						approvals.Add(1)
						_ = rt.DecideApproval(ctx, view.ID, controlruntime.ApprovalDecision{Allowed: true})
					}
				}
			}()
			run, err := rt.StartRun(ctx, controlruntime.Input{Text: "Verify configured services using executor"})
			if err != nil {
				t.Fatal(err)
			}
			if err := rt.WaitRun(ctx, run); err != nil {
				t.Fatal(err)
			}
			select {
			case <-reviewed:
			case <-ctx.Done():
				tasks, _ := rt.BackgroundTasks("")
				t.Fatalf("executor did not complete: tasks=%+v; %s", tasks, runtimeTestDiagnostic(rt))
			}
			wantCalls := int32(2)
			if deny {
				wantCalls = 0
			}
			if mcpCalls.Load() != wantCalls || mediaCalls.Load() != 2 {
				t.Fatalf("MCP calls=%d media calls=%d", mcpCalls.Load(), mediaCalls.Load())
			}
			if id, ok := resumedID.Load().(string); ok {
				metadata, err := session.ReadSubagentMetadata(rt.CurrentSessionID(), id)
				if err != nil || metadata.RunCount != 2 || metadata.ToolCeiling == nil {
					t.Errorf("executor resume did not preserve task scope: %+v %v", metadata, err)
				}
			} else {
				t.Error("executor was not resumed")
			}
			if !deny && approvals.Load() == 0 {
				t.Fatal("executor bypassed manual approval")
			}
		})
	}
}
