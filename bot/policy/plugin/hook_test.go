package plugin

import (
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
