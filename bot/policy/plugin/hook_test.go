package plugin

import (
	"nekocode/protocol"
	"strings"
	"testing"
)

func TestValidatePluginOutputRejectsSchemaFailures(t *testing.T) {
	err := validatePluginOutput([]byte(`{"type":"object","required":["name"]}`), `{"age":1}`)
	if err == nil {
		t.Fatal("missing required field should fail")
	}
	if !strings.Contains(err.Error(), "schema validation failed") {
		t.Fatalf("error = %q, want schema validation failure", err.Error())
	}
}

func TestRunFirstPluginActionRejectsUnsupportedActionTypes(t *testing.T) {
	got := runPluginActions(t.TempDir(), "PreToolUse", Event{}, []hookAction{{Type: "unknown", Command: "echo bad"}})
	if got == nil || got.Err == nil {
		t.Fatalf("unsupported action result = %+v, want error", got)
	}
}

func TestFailureHookSeesNonzeroProcessExit(t *testing.T) {
	h := makePluginHook("post_tool_use", "PostToolUseFailure", t.TempDir(), eventHook{Hooks: []hookAction{{Type: "js", Code: "JSON.stringify([event.error,event.process.exit_code])"}}}, true)
	for _, code := range []int{0, 7} {
		got := h.On(Event{Tool: "shell", Process: protocol.ProcessObservation{State: "exited", ExitCode: code, ExitCodeKnown: true}})
		if code == 0 && got != nil {
			t.Fatalf("successful process triggered failure hook: %+v", got)
		}
		if code != 0 && (got == nil || got.Err != nil || got.Hint == nil || got.Hint.Content != "[false,7]") {
			t.Fatalf("unexpected failure hook result: %+v", got)
		}
	}
}
