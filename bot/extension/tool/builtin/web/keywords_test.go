package web

import (
	"context"
	"nekocode/bot/extension/tool/runtime/core"
	"net/http"
	"strings"
	"testing"
)

func TestWebToolsLiteralKeywords(t *testing.T) {
	const page = "<main><h1>Title</h1><p>intro</p><p>needle data</p><p>adjacent</p><p>omit this unrelated tail</p></main>"
	client := &http.Client{Transport: webRoundTripFunc(func(*http.Request) (*http.Response, error) { return stubResponse(200, page), nil })}
	for _, tool := range []interface {
		Execute(ctx context.Context, args map[string]any) (core.ToolOutput, error)
	}{
		&WebFetchTool{client: client}, &WebExtractTool{client: client},
	} {
		out, err := tool.Execute(t.Context(), map[string]any{"url": testTargetURL, "keywords": "needle"})
		if err != nil || !strings.Contains(out.Text, "needle data") || strings.Contains(out.Text, "unrelated tail") || out.Artifact.Path == "" {
			t.Fatalf("keywords not applied or full source lost: %q %v", out.Text, err)
		}
		for _, args := range []map[string]any{{"keywords": 42}, {"keywords": nil}, {"keywords": "needle", "prompt": "different"}, {"prompt": false}} {
			args["url"] = testTargetURL
			if _, err := tool.Execute(t.Context(), args); err == nil {
				t.Fatalf("invalid keywords accepted: %#v", args)
			}
		}
	}
}
