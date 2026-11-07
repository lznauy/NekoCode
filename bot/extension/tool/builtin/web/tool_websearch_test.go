package web

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
)

func TestSearchResultCountValidation(t *testing.T) {
	t.Setenv("EXA_API_KEY", "")
	t.Setenv("TAVILY_API_KEY", "")
	for _, value := range []any{0.0, -1.0, 0.5, 2.5, math.NaN(), math.Inf(1), "8"} {
		tool := &WebSearchTool{client: &http.Client{Transport: webRoundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Errorf("invalid result count %v reached network", value)
			return stubResponse(400, "bad request"), nil
		})}}
		if _, err := tool.Execute(context.Background(), map[string]any{"query": "test", "numResults": value}); err == nil {
			t.Errorf("invalid result count %v accepted", value)
		}
	}
	for _, value := range []any{3, 3.0, 1e100} {
		tool := &WebSearchTool{client: &http.Client{Transport: webRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			var payload struct {
				Params struct {
					Arguments struct {
						NumResults int `json:"numResults"`
					} `json:"arguments"`
				} `json:"params"`
			}
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			want := 3
			if value == 1e100 {
				want = 15
			}
			if payload.Params.Arguments.NumResults != want {
				t.Errorf("value=%v sent %d, want %d", value, payload.Params.Arguments.NumResults, want)
			}
			r := stubResponse(200, `{"result":{"content":[{"text":"source"}]}}`)
			r.Header.Set("Content-Type", "application/json")
			return r, nil
		})}}
		if _, err := tool.Execute(context.Background(), map[string]any{"query": "test", "numResults": value}); err != nil {
			t.Error(err)
		}
	}
}

func TestSearchResponseFormats(t *testing.T) {
	t.Setenv("EXA_API_KEY", "")
	t.Setenv("TAVILY_API_KEY", "")
	payload := `{"result":{"content":[{"text":"first source"},{"text":"second source"}]}}`
	for _, stream := range []string{
		"data: " + payload + "\n\n",
		": keepalive\n\ndata:{\"method\":\"notifications/progress\"}\n\ndata:" + payload,
		"data: {\"result\":\n" + "data: {\"content\":[{\"text\":\"first source\"},{\"text\":\"second source\"}]}}\n\n",
	} {
		got, err := parseExaSSE(strings.NewReader(stream))
		if err != nil || got != "first source\n\nsecond source" {
			t.Fatalf("got %q, %v", got, err)
		}
	}
	client := &http.Client{Transport: &stubTransport{origin: func() (*http.Response, error) {
		resp := stubResponse(200, payload)
		resp.Header.Set("Content-Type", "application/json; charset=utf-8")
		return resp, nil
	}}}
	got, err := (&WebSearchTool{client: client}).Execute(context.Background(), map[string]any{"query": "sources"})
	if err != nil || !strings.Contains(got.Text, "second source") {
		t.Fatalf("JSON response: %+v, %v", got, err)
	}
}

func TestSearchStreamLongResponseAndErrors(t *testing.T) {
	long, _ := json.Marshal(map[string]any{"result": map[string]any{"content": []map[string]string{{"text": strings.Repeat("中", 30000)}}}})
	got, err := parseExaSSE(strings.NewReader("data: " + string(long) + "\n\n"))
	if err != nil || len([]rune(got)) < 10000 {
		t.Fatalf("long response: %d runes, %v", len([]rune(got)), err)
	}
	if len([]rune(got)) != 30000 {
		t.Fatal("search parser must preserve content before the delivery layer applies its budget")
	}
	for _, stream := range []string{
		"", ": keepalive\n\n", "data: invalid\n\n",
		`data: {"error":{"code":-1,"message":"unavailable"}}`,
		`data: {"result":{"isError":true,"content":[{"text":"rate limited"}]}}`,
		`data: {"result":{"content":[]}}`,
		"data: " + strings.Repeat("x", maxSearchBody+1),
	} {
		if _, err := parseExaSSE(strings.NewReader(stream)); err == nil {
			t.Fatalf("expected error for %.80q", stream)
		}
	}
	if _, err := (&WebSearchTool{}).Execute(context.Background(), map[string]any{"query": "  "}); err == nil {
		t.Fatal("blank query accepted")
	}
}

func TestSearchRetryReplaysRequestBody(t *testing.T) {
	t.Setenv("EXA_API_KEY", "")
	t.Setenv("TAVILY_API_KEY", "")
	calls := 0
	var first string
	client := &http.Client{Transport: webRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(req.Body)
		calls++
		if calls == 1 {
			first = string(body)
			resp := stubResponse(503, "busy")
			resp.Header.Set("Retry-After", "0")
			return resp, nil
		}
		if string(body) != first || !strings.Contains(string(body), "test query") {
			t.Errorf("retry lost request body: %s", body)
		}
		resp := stubResponse(200, `{"result":{"content":[{"text":"source"}]}}`)
		resp.Header.Set("Content-Type", "application/json")
		return resp, nil
	})}
	_, err := (&WebSearchTool{client: client}).Execute(context.Background(), map[string]any{"query": "test query"})
	if err != nil || calls != 2 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestWebSearchTool(t *testing.T) {
	ws := &WebSearchTool{}
	_, err := ws.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error for missing query")
	}
}
