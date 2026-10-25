package plugin

import (
	"context"
	"encoding/json"
	"nekocode/bot/policy/contract"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLoadRejectsInvalidConfiguration(t *testing.T) {
	for _, config := range []string{
		`{"Unknown":[]}`,
		`{"version":2}`,
		`{"PreToolUse":[{"hooks":[{"type":"unknown"}]}]}`,
		`{"PreToolUse":[{"matcher":"[","hooks":[{"type":"command","command":"true"}]}]}`,
		`{"PreToolUse":[{"hooks":[{"type":"command","command":"true","timeout":-1}]}]}`,
		`{"PreToolUse":[{"hooks":[{"type":"js","code":"true","path":"a.js"}]}]}`,
		`{"PreToolUse":[{"unknown":true,"hooks":[{"type":"js","code":"null"}]}]}`,
		`{"PreToolUse":[{"id":"same","hooks":[{"type":"js","code":"null"}]},{"id":"same","hooks":[{"type":"js","code":"null"}]}]}`,
		`{"PostToolUse":[{"mode":"guard","hooks":[{"type":"js","code":"null"}]}]}`,
	} {
		t.Run(config, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "hooks.json"), []byte(config), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(root, "hooks.json"); err == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
}

func TestGeneratedIDSurvivesUnrelatedEntry(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "hooks.json")
	load := func(config string) []Hook {
		t.Helper()
		if err := os.WriteFile(path, []byte(config), 0600); err != nil {
			t.Fatal(err)
		}
		h, err := Load(root, "hooks.json")
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	before := load(`{"Stop":[{"hooks":[{"type":"js","code":"null"}]}]}`)
	after := load(`{"PreModel":[{"hooks":[{"type":"js","code":"null"}]}],"Stop":[{"hooks":[{"type":"js","code":"null"}]}]}`)
	if before[0].Name != after[1].Name {
		t.Fatalf("identity changed: %s -> %s", before[0].Name, after[1].Name)
	}
}

func TestCommandReceivesEventAndReturnsDecision(t *testing.T) {
	root := t.TempDir()
	event := Event{Version: 1, Point: "pre_tool_use", SessionID: "session", RunID: "run", ActorID: "main", Tool: "write", Args: map[string]any{"path": "main.go"}}
	result := runPluginActions(root, "PreToolUse", event, []hookAction{{Type: "command", OutputFormat: "json", Command: `cat > input.json; printf '%s' '{"decision":{"action":"reject","reason":"read first"},"state_patch":{"ints":{"count":1}}}'`}})
	if result == nil || result.Err != nil || result.Decision == nil || result.Decision.Reason != "read first" {
		t.Fatalf("result=%+v", result)
	}
	input, err := os.ReadFile(filepath.Join(root, "input.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{`"tool":"write"`, `"path":"main.go"`, `"run_id":"run"`} {
		if !strings.Contains(string(input), part) {
			t.Fatalf("missing %s in %s", part, input)
		}
	}
}

func TestAllActionsRunAndMergeHints(t *testing.T) {
	result := runPluginActions(t.TempDir(), "PreToolUse", Event{}, []hookAction{
		{Type: "js", Code: `({hint:{content:"first"}})`},
		{Type: "js", Code: `({hint:{content:"second"},decision:{action:"reject",reason:"blocked"}})`},
	})
	if result == nil || result.Err != nil || len(result.Hints) != 2 || result.Decision == nil || result.Decision.Action != "reject" {
		t.Fatalf("result=%+v", result)
	}
}

func TestJSAndCommandHonorParentCancellation(t *testing.T) {
	for _, kind := range []string{"js", "command"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
			defer cancel()
			start := time.Now()
			action := hookAction{Type: kind, Code: `while(true){}`, Command: "sleep 20 & wait"}
			result := runPluginActions(t.TempDir(), "PreToolUse", Event{Context: ctx}, []hookAction{action})
			if result == nil || result.Err == nil || time.Since(start) > time.Second {
				t.Fatalf("cancellation result=%+v elapsed=%v", result, time.Since(start))
			}
		})
	}
}

func TestStructuredResultRejectsTyposAndConflicts(t *testing.T) {
	for _, raw := range []string{`{"decison":{"action":"reject"}}`, `{"decision":{"action":"unknown"}}`, `{"decision":{"action":"reject"},"block_tool":{}}`, `{"hint":{"contnet":"lost"}}`} {
		if _, err := decodeStructuredResult([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestLegacyPostToolRequirementsSurviveMultipleActions(t *testing.T) {
	got := runPluginActions(t.TempDir(), "PostToolUse", Event{}, []hookAction{
		{Type: "js", Code: `({require_tool:{tool:"verify",reason:"check changes"},state_patch:{ints:{count:1}}})`},
		{Type: "js", Code: `({hint:{content:"another hint"}})`},
	})
	if got == nil || got.Err != nil || got.Decision != nil || len(got.Hints) != 2 || got.StatePatch.Ints["count"] != 1 {
		t.Fatalf("result=%+v", got)
	}
}

func TestCommandOutputProtocolIsExplicit(t *testing.T) {
	for _, tc := range []struct {
		name, config string
		wantError    bool
		wantText     string
	}{
		{"legacy JSON is text", `{"PreToolUse":[{"hooks":[{"type":"command","command":"printf '%s' '{\"status\":\"ok\"}'"}]}]}`, false, `{"status":"ok"}`},
		{"v1 rejects non-protocol text", `{"version":1,"PreToolUse":[{"hooks":[{"type":"command","command":"printf ordinary-text"}]}]}`, true, ""},
		{"v1 explicit text", `{"version":1,"PreToolUse":[{"hooks":[{"type":"command","output_format":"text","command":"printf '%s' '{\"status\":\"ok\"}'"}]}]}`, false, `{"status":"ok"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "hooks.json"), []byte(tc.config), 0600); err != nil {
				t.Fatal(err)
			}
			hooks, err := Load(root, "hooks.json")
			if err != nil {
				t.Fatal(err)
			}
			got := hooks[0].On(Event{})
			if got == nil || (got.Err != nil) != tc.wantError {
				t.Fatalf("result=%+v", got)
			}
			if tc.wantText != "" && (got.Hint == nil || !strings.Contains(got.Hint.Content, tc.wantText)) {
				t.Fatalf("text output lost: %+v", got)
			}
		})
	}
}

func TestJSProgramIsSnapshotUntilReloadAndVMsArePrivate(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "hook.js")
	if err := os.WriteFile(script, []byte(`var count = typeof count === "undefined" ? 1 : count+1; hook=function(){return {hint:{content:String(count)}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "hooks.json"), []byte(`{"PreModel":[{"hooks":[{"type":"js","path":"hook.js"}]}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	hooks, err := Load(root, "hooks.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte(`({hint:{content:"reloaded"}})`), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		got := hooks[0].On(Event{})
		if got == nil || got.Err != nil || got.Hint == nil || got.Hint.Content != "1" {
			t.Fatalf("program snapshot or VM isolation lost: %+v", got)
		}
	}
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			got := hooks[0].On(Event{})
			if got == nil || got.Err != nil || got.Hint == nil || got.Hint.Content != "1" {
				t.Error("compiled program leaked state between VMs")
			}
		}()
	}
	workers.Wait()
	hooks, err = Load(root, "hooks.json")
	if err != nil {
		t.Fatal(err)
	}
	if got := hooks[0].On(Event{}); got == nil || got.Hint == nil || got.Hint.Content != "reloaded" {
		t.Fatalf("reload did not update program: %+v", got)
	}
}

func TestCommandAndJSEventsHaveIdenticalDefaults(t *testing.T) {
	root := t.TempDir()
	event := Event{SessionID: "session", RunID: "run", HookID: "hook", Point: "pre_model"}
	output, _, err := runPluginCommandEvent(root, event, hookAction{Command: "printf warning >&2; cat"})
	if err != nil {
		t.Fatal(err)
	}
	js, err := runPluginJS(root, event, hookAction{Type: "js", Code: `JSON.stringify(event)`})
	if err != nil {
		t.Fatal(err)
	}
	var commandEvent, jsEvent map[string]any
	if err := json.Unmarshal([]byte(output), &commandEvent); err != nil {
		t.Fatal(err)
	}
	if js == nil || js.Hint == nil {
		t.Fatalf("JS result=%+v", js)
	}
	if err := json.Unmarshal([]byte(js.Hint.Content), &jsEvent); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(commandEvent, jsEvent) {
		t.Fatalf("event mismatch: command=%v JS=%v", commandEvent, jsEvent)
	}
	state := commandEvent["state"].(map[string]any)
	if state["ints"] == nil || state["strings"] == nil || commandEvent["args"] == nil {
		t.Fatal("empty containers must be objects")
	}
}

func FuzzDecodeStructuredResult(f *testing.F) {
	for _, seed := range []string{`{}`, `null`, `{"decision":{"action":"reject","reason":"deny"}}`, `{"stop":{"reason":"completed"}}`, `{"hint":{"content":"x"},"blockTool":{}}`, `{"state_patch":{"ints":{"x":9223372036854775808}}}`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > maxPluginOutputBytes {
			return
		}
		result, err := decodeStructuredResult([]byte(raw))
		if err != nil {
			return
		}
		if result == nil {
			t.Fatal("accepted nil result")
		}
		original, err := contract.Control(*result)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := decodeStructuredResult(encoded)
		if err != nil {
			t.Fatalf("accepted result cannot round trip: %v", err)
		}
		roundTrip, err := contract.Control(*decoded)
		if err != nil || roundTrip != original {
			t.Fatalf("decision changed after round trip: %+v %+v %v", original, roundTrip, err)
		}
	})
}

func TestLegacyToolScopeIsStableAcrossActionCount(t *testing.T) {
	for _, tool := range []string{"read", "write"} {
		for _, multiple := range []bool{false, true} {
			actions := []hookAction{{Type: "js", Code: `({block_tool:{tool:"write",reason:"write only"},state_patch:{ints:{count:1}}})`}}
			if multiple {
				actions = append(actions, hookAction{Type: "js", Code: `({hint:{content:"independent"}})`})
			}
			result := runPluginActions(t.TempDir(), "PreToolUse", Event{Tool: tool}, actions)
			if result == nil || result.Err != nil {
				t.Fatalf("result=%+v", result)
			}
			d, err := contract.Control(*result)
			if err != nil {
				t.Fatal(err)
			}
			if (d.Action == contract.Reject) != (tool == "write") {
				t.Fatalf("tool=%s multiple=%t decision=%+v", tool, multiple, d)
			}
			if result.StatePatch == nil || result.StatePatch.Ints["count"] != 1 {
				t.Fatal("tool scope filtering lost state")
			}
		}
	}
}

func TestLegacyTextOutputKeepsTruncation(t *testing.T) {
	result := runPluginActions(t.TempDir(), "PreToolUse", Event{}, []hookAction{{Type: "command", Command: `awk 'BEGIN {for(i=0;i<10000;i++) printf "x"}'`}})
	if result == nil || result.Err != nil || result.Hint == nil || !strings.Contains(result.Hint.Content, `truncated="true"`) {
		t.Fatalf("legacy text truncation changed: %+v", result)
	}
}

func TestAmbiguousJSONIsRejected(t *testing.T) {
	for _, raw := range []string{
		`{"decision":{"action":"reject","action":"continue"}}`,
		`{"decision":{"action":"reject"},"decision":{"action":"continue"}}`,
		`{"state_patch":{"ints":{"n":1,"n":2}}}`,
	} {
		if _, err := decodeStructuredResult([]byte(raw)); err == nil {
			t.Fatalf("duplicate keys accepted: %s", raw)
		}
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hooks.json"), []byte(`{"PreToolUse":[{"hooks":[{"type":"js","code":"null"}]}],"PreToolUse":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root, "hooks.json"); err == nil {
		t.Fatal("duplicate event configuration accepted")
	}
}

func TestUnsupportedSchemaFailsDuringLoad(t *testing.T) {
	for _, schema := range []string{`{"type":"typo"}`, `{"type":7}`, `{"required":[false]}`, `{"properties":{"count":{"minimum":1}}}`} {
		root := t.TempDir()
		config := `{"PreModel":[{"hooks":[{"type":"js","code":"null","output_schema":` + schema + `}]}]}`
		if err := os.WriteFile(filepath.Join(root, "hooks.json"), []byte(config), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(root, "hooks.json"); err == nil {
			t.Fatalf("unsupported schema accepted: %s", schema)
		}
	}
}

func TestPluginFileAndJSONLimits(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "hooks.json")
	if err := os.WriteFile(config, []byte(strings.Repeat(" ", maxPluginSourceBytes+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root, "hooks.json"); err == nil {
		t.Fatal("oversized config accepted")
	}
	nested := strings.Repeat("[", maxJSONDepth+2) + "0" + strings.Repeat("]", maxJSONDepth+2)
	var value any
	if err := strictJSON([]byte(nested), &value); err == nil {
		t.Fatal("excessive nesting accepted")
	}
	entries := strings.Repeat(`{"hooks":[{"type":"command","command":"true"}]},`, 256) + `{"hooks":[{"type":"command","command":"true"}]}`
	if err := os.WriteFile(config, []byte(`{"PreModel":[`+entries+`]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root, "hooks.json"); err == nil {
		t.Fatal("oversized hook registry accepted")
	}
	if err := os.WriteFile(filepath.Join(root, "large"), []byte(strings.Repeat("x", maxPluginReadFileBytes+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPluginFile(root, "large"); err == nil {
		t.Fatal("oversized readFile accepted")
	}
	if _, err := readLimitedFile(root, maxPluginSourceBytes); err == nil {
		t.Fatal("directory accepted as plugin file")
	}
}

func TestEventCapabilitiesPreservePluginBehavior(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "hooks.json")
	load := func(name, mode, matcher string) ([]Hook, error) {
		t.Helper()
		config, err := json.Marshal(map[string]any{name: []any{map[string]any{"mode": mode, "matcher": matcher, "hooks": []any{map[string]any{"type": "js", "code": "null"}}}}})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, config, 0600); err != nil {
			t.Fatal(err)
		}
		return Load(root, "hooks.json")
	}
	guards := map[Point]bool{UserSubmit: true, PreModel: true, PreToolUse: true, Stop: true}
	seen := map[string]bool{}
	for _, event := range eventSpecs {
		if seen[event.Name] {
			t.Fatalf("duplicate event %s", event.Name)
		}
		seen[event.Name] = true
		hooks, err := load(event.Name, "", "")
		if err != nil || len(hooks) != 1 {
			t.Fatalf("%s: hooks=%v err=%v", event.Name, hooks, err)
		}
		h := hooks[0]
		if h.Point != event.Point || h.Guard != (event.Point == PreToolUse || event.Point == Stop) || h.Once != (event.Point == SessionStart) || h.SessionScoped != (event.Point == SessionStart || event.Point == SessionEnd) {
			t.Fatalf("changed defaults at %s: %+v", event.Name, h)
		}
		if _, err := load(event.Name, "guard", ""); (err == nil) != guards[event.Point] {
			t.Errorf("guard mode at %s: %v", event.Name, err)
		}
		if _, err := load(event.Name, "observer", "write"); (err == nil) != (event.Point == PreToolUse || event.Point == PostToolUse) {
			t.Errorf("tool matcher at %s: %v", event.Name, err)
		}
	}
	if len(seen) != 15 || !seen["PostToolUseFailure"] {
		t.Fatalf("configuration events changed: %v", seen)
	}
}
