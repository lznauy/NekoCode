package headless

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nekocode/protocol"
	rt "nekocode/runtime"
)

type onceBuffer struct{ bytes.Buffer }

func (*onceBuffer) Close() error { return nil }

func onceBackend(t *testing.T, runner rt.RunnerFunc) *rt.Runtime {
	t.Helper()
	backend := rt.New(runner, rt.ServiceGroups{Sessions: rt.SessionsServices{
		NewSession:       func() (rt.SessionMeta, error) { return rt.SessionMeta{ID: "once-session"}, nil },
		CurrentSessionID: func() string { return "once-session" },
		ListSessions:     func() []rt.SessionMeta { return nil },
		SessionMessages:  func() []rt.DisplayMessage { return nil },
	}})
	t.Cleanup(func() { _ = backend.Close() })
	return backend
}

func TestOnceFinalOutput(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			backend := onceBackend(t, func(_ context.Context, input string, host rt.RunHost) (string, error) {
				if input != "task from stdin" {
					t.Errorf("input=%q", input)
				}
				host.Text("discarded preview")
				host.Step(protocol.StepEvent{Action: protocol.StepActionExecuteTool, CallID: "test", ToolName: "read", Output: "tool secret"})
				return "final answer", nil
			})
			out := &onceBuffer{}
			err := serveOnce(t.Context(), io.NopCloser(strings.NewReader("task from stdin")), out, backend, "/workspace", Options{OutputFormat: format})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out.String(), "preview") || strings.Contains(out.String(), "secret") {
				t.Fatalf("intermediate output leaked: %s", out.String())
			}
			if format == "text" {
				if out.String() != "final answer\n" {
					t.Fatal(out.String())
				}
			} else {
				var result map[string]any
				if err := json.Unmarshal(out.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if result["result"] != "final answer" || result["is_error"] != false || result["session_id"] != "once-session" {
					t.Fatal(result)
				}
			}
		})
	}
}

func TestOnceFailures(t *testing.T) {
	for _, kind := range []string{"runner", "step_limit", "approval", "question", "cancelled", "backend_cancelled"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			backend := onceBackend(t, func(ctx context.Context, _ string, host rt.RunHost) (string, error) {
				switch kind {
				case "runner":
					return "partial", errors.New("model unavailable")
				case "backend_cancelled":
					host.Step(protocol.StepEvent{Action: protocol.StepActionRunSummary, Summary: &protocol.RunSummary{StopReason: "cancelled"}})
				case "step_limit":
					host.Step(protocol.StepEvent{Action: protocol.StepActionRunSummary, Summary: &protocol.RunSummary{StepCount: 4, StopReason: "step_limit"}})
				case "approval":
					if host.Confirm(rt.ConfirmRequest{ToolName: "shell"}).Allowed {
						t.Error("approval unexpectedly allowed")
					}
				case "question":
					host.Ask(rt.QuestionRequest{Questions: []rt.QuestionItem{{Question: "choose"}}})
				case "cancelled":
					cancel()
					<-ctx.Done()
					return "", ctx.Err()
				}
				if (kind == "approval" || kind == "question") && ctx.Err() == nil {
					t.Error("runner resumed before cancellation")
				}
				return "incomplete", nil
			})
			out := &onceBuffer{}
			err := serveOnce(ctx, io.NopCloser(strings.NewReader("")), out, backend, "/workspace", Options{Prompt: "run", OutputFormat: "json"})
			want := 1
			if kind == "cancelled" || kind == "backend_cancelled" {
				want = 130
			}
			if ExitCode(err) != want {
				t.Fatalf("exit=%d err=%v output=%s", ExitCode(err), err, out.String())
			}
			if kind != "cancelled" {
				var result map[string]any
				if json.Unmarshal(out.Bytes(), &result) != nil || result["is_error"] != true {
					t.Fatalf("missing failure JSON: %s", out.String())
				}
			}
		})
	}
}

func writeOnceSchema(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "schema.json")
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestOnceSchema(t *testing.T) {
	schema := writeOnceSchema(t, `{"type":"object","properties":{"count":{"$ref":"#/$defs/count"}},"required":["count"],"additionalProperties":false,"$defs":{"count":{"type":"integer"}}}`)
	tests := []struct {
		name, answer, subtype string
		exit                  int
	}{
		{name: "success", answer: `{"count":9007199254740993}`, subtype: "success"},
		{name: "invalid_json", answer: "```json\n{\"count\":1}\n```", subtype: "error_output_validation", exit: 1},
		{name: "extra_json_value", answer: `{"count":1} {"count":2}`, subtype: "error_output_validation", exit: 1},
		{name: "trailing_content", answer: `{"count":1} garbage`, subtype: "error_output_validation", exit: 1},
		{name: "schema_mismatch", answer: `{"count":"wrong"}`, subtype: "error_output_validation", exit: 1},
		{name: "missing_required_property", answer: `{}`, subtype: "error_output_validation", exit: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runs := 0
			backend := onceBackend(t, func(_ context.Context, input string, _ rt.RunHost) (string, error) {
				runs++
				if !strings.Contains(input, "exactly one JSON value") || !strings.Contains(input, `"$defs"`) {
					t.Errorf("schema missing in prompt: %q", input)
				}
				return tc.answer, nil
			})
			out := &onceBuffer{}
			err := serveOnce(t.Context(), io.NopCloser(strings.NewReader("")), out, backend, "/workspace", Options{Prompt: "count", OutputFormat: "json", OutputSchema: schema})
			if ExitCode(err) != tc.exit {
				t.Fatalf("exit=%d err=%v output=%s", ExitCode(err), err, out.String())
			}
			if runs != 1 {
				t.Fatalf("runner calls=%d, want 1", runs)
			}
			var result map[string]any
			if json.Unmarshal(out.Bytes(), &result) != nil {
				t.Fatalf("invalid result JSON: %s", out.String())
			}
			if result["subtype"] != tc.subtype || result["is_error"] != (tc.exit != 0) {
				t.Fatalf("result=%v", result)
			}
			if tc.exit == 0 {
				if !strings.Contains(out.String(), `"structured_output":{"count":9007199254740993}`) {
					t.Fatalf("lost numeric precision: %s", out.String())
				}
			} else if _, ok := result["structured_output"]; ok {
				t.Fatalf("failed validation returned structured_output: %v", result)
			}
		})
	}
	for _, text := range []string{`{`, `{"type":"invalid"}`, `{"$ref":"https://example.com/schema.json"}`, `{"$ref":"other.json"}`, `{"$schema":"https://example.com/custom-meta-schema"}`, `null`} {
		if _, err := prepareOnce(t.Context(), Options{OutputSchema: writeOnceSchema(t, text)}); err == nil {
			t.Fatalf("accepted invalid/unsupported schema: %s", text)
		}
	}
}

func TestOnceInvalidSchemaDoesNotStartRun(t *testing.T) {
	for _, tc := range []struct{ name, path string }{
		{name: "missing_file", path: filepath.Join(t.TempDir(), "missing.json")},
		{name: "compile_error", path: writeOnceSchema(t, `{"type":"invalid"}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := onceBackend(t, func(context.Context, string, rt.RunHost) (string, error) {
				t.Error("invalid schema started run")
				return "", nil
			})
			out := &onceBuffer{}
			err := serveOnce(t.Context(), io.NopCloser(strings.NewReader("")), out, backend, "/workspace", Options{Prompt: "count", OutputFormat: "json", OutputSchema: tc.path})
			if ExitCode(err) != 2 || out.Len() != 0 {
				t.Fatalf("err=%v output=%s", err, out.String())
			}
		})
	}
}

func TestOnceCancellationOverridesOutputValidation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	backend := onceBackend(t, func(context.Context, string, rt.RunHost) (string, error) {
		cancel()
		return "not JSON", nil
	})
	out := &onceBuffer{}
	err := serveOnce(ctx, io.NopCloser(strings.NewReader("")), out, backend, "/workspace", Options{
		Prompt: "count", OutputFormat: "json", OutputSchema: writeOnceSchema(t, `{"type":"object"}`),
	})
	if ExitCode(err) != 130 {
		t.Fatalf("exit=%d err=%v output=%s", ExitCode(err), err, out.String())
	}
	var result map[string]any
	if json.Unmarshal(out.Bytes(), &result) != nil || result["subtype"] != "error_cancelled" || result["is_error"] != true {
		t.Fatalf("cancellation did not take priority: %s", out.String())
	}
}

func TestOnceInvalidInputDoesNotStartRun(t *testing.T) {
	for _, input := range []string{"", strings.Repeat("x", maxFrameSize+1)} {
		backend := onceBackend(t, func(context.Context, string, rt.RunHost) (string, error) {
			t.Error("invalid input started run")
			return "", nil
		})
		out := &onceBuffer{}
		if err := serveOnce(t.Context(), io.NopCloser(strings.NewReader(input)), out, backend, "/workspace", Options{OutputFormat: "text"}); ExitCode(err) != 2 || out.Len() != 0 {
			t.Fatalf("err=%v output=%s", err, out.String())
		}
	}
}

func TestOnceParseArgs(t *testing.T) {
	for _, tc := range []struct {
		args           []string
		prompt, format string
		invalid        bool
	}{
		{args: []string{"-p", "run"}, prompt: "run", format: "text"},
		{args: []string{"exec", "run", "--output-format=json"}, prompt: "run", format: "json"},
		{args: []string{"exec", "-"}, format: "text"},
		{args: []string{"--print"}, format: "text"},
		{args: []string{"exec", "--resume", "saved", "run"}, prompt: "run", format: "text"},
		{args: []string{"exec", "--", "-literal"}, prompt: "-literal", format: "text"},
		{args: []string{"exec", "run", "extra"}, invalid: true},
		{args: []string{"-p="}, invalid: true},
		{args: []string{"-p", "run", "--include-partial-messages"}, invalid: true},
		{args: []string{"-p", "--input-format=stream-json"}, invalid: true},
		{args: []string{"--headless", "--output-format=json"}, invalid: true},
		{args: []string{"--headless", "--input-format=invalid"}, invalid: true},
		{args: []string{"-p", "run", "--output-schema=x", "--output-format=stream-json"}, invalid: true},
	} {
		options, selected, err := ParseArgs(tc.args)
		if !selected || (err != nil) != tc.invalid {
			t.Fatalf("%v: selected=%v err=%v", tc.args, selected, err)
		}
		if !tc.invalid && (options.Prompt != tc.prompt || options.OutputFormat != tc.format) {
			t.Fatalf("%v: %+v", tc.args, options)
		}
	}
}

func TestOnceStartFailure(t *testing.T) {
	out := &onceBuffer{}
	err := serveOnce(t.Context(), io.NopCloser(strings.NewReader("")), out, stdioTestBackend{}, "/workspace", Options{Prompt: "run", OutputFormat: "json"})
	if ExitCode(err) != 1 || !strings.Contains(out.String(), "start rejected") {
		t.Fatalf("start failure: %v %s", err, out.String())
	}
}
