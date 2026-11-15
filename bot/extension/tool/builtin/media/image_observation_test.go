package media

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nekocode/bot/calllog"
	"nekocode/bot/config"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/workspace"
	"nekocode/protocol"
)

func TestImageModelOptionsExposeNamesWithoutConfigurationSecrets(t *testing.T) {
	tool := NewImageUnderstandTool([]config.ImageUnderstandConfig{{Name: "fast", Model: "internal-model", APIKey: "private-config-sentinel", BaseURL: "https://private.example"}})
	b, _ := json.Marshal(tool.Parameters())
	if !strings.Contains(string(b), `"fast"`) || strings.Contains(string(b), "private-config-sentinel") || strings.Contains(string(b), "private.example") {
		t.Fatal("unsafe model discovery schema")
	}
	if !strings.Contains(tool.Description(), "never read application configuration") {
		t.Fatal("missing safe discovery guidance")
	}
}

func TestImageStreamLifetimeBelongsToRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"visible\"},\"finish_reason\":\"stop\"}]}\n\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, path := imageUnderstandTestContext(t)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	tool := NewImageUnderstandTool([]config.ImageUnderstandConfig{{Name: "fast", Model: "vision", BaseURL: server.URL}})
	tool.client.Timeout = 50 * time.Millisecond
	out, err := tool.Execute(ctx, map[string]any{"path": path})
	if err == nil || out.Text != "" {
		t.Fatalf("unterminated stream claimed success: text=%q err=%v", out.Text, err)
	}
}

func TestImageCallRecordsUsageIdentityAndUnknownFailures(t *testing.T) {
	for _, kind := range []string{"json", "sse", "sse-tail", "deepseek", "anthropic", "quota", "usage-omitted"} {
		t.Run(kind, func(t *testing.T) {
			var records []calllog.Record
			original := writeImageCallRecord
			writeImageCallRecord = func(r calllog.Record) { records = append(records, r) }
			t.Cleanup(func() { writeImageCallRecord = original })
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch kind {
				case "quota":
					w.WriteHeader(429)
					_, _ = w.Write([]byte(`{"secret":"provider-private-sentinel"}`))
				case "sse":
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = w.Write([]byte("data: {\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":7,\"prompt_tokens_details\":{\"cached_tokens\":60},\"completion_tokens_details\":{\"reasoning_tokens\":2}},\"choices\":[{\"delta\":{\"content\":\"visible\"},\"finish_reason\":\"stop\"}]}\n\n"))
				case "sse-tail":
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"visible\"},\"finish_reason\":\"stop\"}]}\n\n"))
					w.(http.Flusher).Flush()
					time.Sleep(1100 * time.Millisecond) // Usage remains part of the request, beyond the former one-second cutoff.
					_, _ = w.Write([]byte("data: {\"choices\":[],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":7,\"prompt_tokens_details\":{\"cached_tokens\":60},\"completion_tokens_details\":{\"reasoning_tokens\":2}}}\n\ndata: [DONE]\n\n"))
				case "deepseek":
					_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"visible"}}],"usage":{"prompt_tokens":100,"completion_tokens":7,"prompt_cache_hit_tokens":40,"prompt_cache_miss_tokens":60}}`))
				case "anthropic":
					_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"visible"}],"usage":{"input_tokens":100,"output_tokens":7}}`))
				case "usage-omitted":
					_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"visible"}}]}`))
				default:
					_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"visible"}}],"usage":{"prompt_tokens":100,"completion_tokens":7,"prompt_tokens_details":{"cached_tokens":60},"completion_tokens_details":{"reasoning_tokens":2}}}`))
				}
			}))
			defer server.Close()
			ctx, path := imageUnderstandTestContext(t)
			manager, _ := workspace.FromContext(ctx)
			manager.SetSession("usage-session")
			ctx = calllog.WithSession(protocol.WithExecutionIdentity(core.WithToolCallID(ctx, "parent-tool"), protocol.ExecutionIdentity{RunID: "usage-run"}), "usage-session")
			manager.SetSession("different-workspace-task-id")
			protocolName := "openai"
			if kind == "anthropic" {
				protocolName = "anthropic"
			}
			tool := NewImageUnderstandTool([]config.ImageUnderstandConfig{{Name: "fast", Model: "vision-test", APIKey: "request-private-sentinel", Protocol: protocolName, BaseURL: server.URL}})
			_, err := tool.Execute(ctx, map[string]any{"path": path})
			if len(records) != 1 {
				t.Fatalf("records=%d", len(records))
			}
			record := records[0]
			if record.SessionID != "usage-session" || record.Execution.RunID != "usage-run" || record.ToolCallID != "parent-tool" || record.Source != "image_understand" {
				t.Fatalf("uncorrelated record: %+v", record)
			}
			if record.UsageReported == nil {
				t.Fatal("usage completeness absent")
			}
			if kind == "quota" || kind == "usage-omitted" {
				if *record.UsageReported {
					t.Fatal("missing usage claimed as known zero")
				}
			} else if !*record.UsageReported || record.TotalTokens != 107 {
				t.Fatalf("wrong usage: %+v", record)
			}
			if (kind == "json" || kind == "sse" || kind == "sse-tail") && (!record.CacheUsageReported || record.CachedTokens != 60 || record.ReasoningTokens != 2) {
				t.Fatalf("lost usage details: %+v", record)
			}
			if kind == "deepseek" && (!record.CacheUsageReported || record.CachedTokens != 40 || record.NewTokens != 60) {
				t.Fatalf("lost flat cache fields: %+v", record)
			}
			if kind == "quota" && (err == nil || strings.Contains(err.Error(), "provider-private-sentinel")) {
				t.Fatalf("unsafe error: %v", err)
			}
			b, _ := json.Marshal(record)
			if strings.Contains(string(b), "private-sentinel") {
				t.Fatal("credential/provider body persisted")
			}
		})
	}
}
