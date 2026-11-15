package media

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	utilhttp "nekocode/util/http"
)

func TestImageHTTPErrorPreservesOnlyKnownCategories(t *testing.T) {
	for _, tc := range []struct {
		name, body, category string
		status               int
	}{
		{"openai code", `{"error":{"code":"context_length_exceeded","type":"invalid_request_error","message":"private-sentinel"}}`, "context_length_exceeded", 400},
		{"anthropic type", `{"type":"error","error":{"type":"overloaded_error","message":"private-sentinel"}}`, "overloaded_error", 529},
		{"unknown code known type", `{"error":{"code":"private-sentinel","type":"invalid_request_error"}}`, "invalid_request_error", 400},
		{"unknown category", `{"error":{"code":"private-sentinel","type":"private-sentinel"}}`, "", 429},
		{"malformed", `{"error":{"code":"invalid_api_key"`, "", 401},
		{"html", `<html>private-sentinel</html>`, "", 502},
		{"oversized", `{"error":{"code":"invalid_api_key"},"extra":"` + strings.Repeat("x", 16<<10) + `"}`, "", 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			tool := NewImageUnderstandTool(nil)
			_, err := tool.post(context.Background(), server.URL, nil, nil)
			var httpErr *utilhttp.HTTPError
			if !errors.As(err, &httpErr) || httpErr.StatusCode != tc.status {
				t.Fatalf("HTTP status lost: %v", err)
			}
			want := "image model request failed"
			if tc.category != "" {
				want += ": " + tc.category
			}
			if httpErr.Body != want {
				t.Fatalf("unsafe or missing diagnostic: got %q, want %q", httpErr.Body, want)
			}
		})
	}
}
