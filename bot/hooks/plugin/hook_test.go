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
	if !strings.Contains(err.Error(), "Schema validation failed") {
		t.Fatalf("error = %q, want schema validation failure", err.Error())
	}
}

func TestRunFirstPluginActionSkipsUnsupportedActionTypes(t *testing.T) {
	got := runFirstPluginAction(t.TempDir(), "PreToolUse", Event{}, []hookAction{{Type: "unknown", Command: "echo bad"}})
	if got != nil {
		t.Fatalf("unsupported action result = %+v, want nil", got)
	}
}
