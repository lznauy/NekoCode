package standard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"nekocode/bot/config"
	controlruntime "nekocode/runtime"
)

type testFileConnector struct {
	name  string
	files chan []byte
}

func (c *testFileConnector) Name() string                                            { return c.name }
func (c *testFileConnector) Start(context.Context) error                             { return nil }
func (c *testFileConnector) Stop() error                                             { return nil }
func (c *testFileConnector) HandleCommand(context.Context, []string) (string, error) { return "", nil }
func (c *testFileConnector) DeliverFile(_ context.Context, path string) error {
	data, err := os.ReadFile(path)
	if err == nil {
		c.files <- data
	}
	return err
}

func TestStandardFileDeliveryRepliesToSourceThroughAgent(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("HOME", t.TempDir())
	if err := os.WriteFile("report.txt", []byte("report content"), 0600); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
			Messages json.RawMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			http.Error(w, "bad request", 400)
			return
		}
		found := false
		for _, tool := range body.Tools {
			if tool.Function.Name == "deliver_file" {
				found = true
			}
		}
		if !found {
			t.Error("deliver_file missing from actual model tools")
		}
		delta := map[string]any{"content": "done"}
		finish := "stop"
		if calls.Add(1) == 1 {
			delta = map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "deliver", "type": "function", "function": map[string]any{"name": "deliver_file", "arguments": `{"path":"report.txt"}`}}}}
			finish = "tool_calls"
		} else if !strings.Contains(string(body.Messages), "File delivered to telegram") {
			t.Error("model did not receive successful source delivery result")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, choice := range []any{map[string]any{"index": 0, "delta": delta}, map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finish}} {
			data, _ := json.Marshal(map[string]any{"choices": []any{choice}})
			fmt.Fprintf(w, "data: %s\n\n", data)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer model.Close()
	if err := config.Save(config.Config{Active: "local/test", Providers: []config.ProviderConfig{{Name: "local", APIKey: "test", BaseURL: model.URL + "/v1", Protocol: "openai", Models: []config.ModelDefinition{{Name: "test", Model: "test"}}}}}); err != nil {
		t.Fatal(err)
	}
	rt, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	tg := &testFileConnector{name: "telegram", files: make(chan []byte, 4)}
	fs := &testFileConnector{name: "feishu", files: make(chan []byte, 4)}
	for _, c := range []*testFileConnector{tg, fs} {
		rt.RegisterConnector(c.name, func(controlruntime.ConnectorRuntime) controlruntime.Connector { return c })
		if _, err := rt.Connect(t.Context(), c.name, nil); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	id, err := rt.StartRun(ctx, controlruntime.Input{Source: controlruntime.SourceRef{Kind: "telegram", ID: "123"}, Text: "Send me report.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.WaitRun(ctx, id); err != nil {
		t.Fatal(err)
	}
	run, ok := rt.LookupRun(id)
	if !ok || run.Error != "" || run.Output != "done" || calls.Load() != 2 {
		t.Fatalf("delivery run=%+v calls=%d", run, calls.Load())
	}
	select {
	case data := <-tg.files:
		if string(data) != "report content" {
			t.Fatalf("wrong file: %q", data)
		}
	default:
		t.Fatal("source received no file")
	}
	if len(fs.files) != 0 {
		t.Fatal("implicit delivery broadcast to other connector")
	}
}
