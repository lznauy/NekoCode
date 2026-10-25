package plugin

import (
	"encoding/json"
	"testing"
)

func TestSchemaOutputJSONBoundary(t *testing.T) {
	for _, output := range []string{`{"key":"value"}`, `[1,2,3]`, `"hello"`, `42`, `true`, `null`} {
		if err := validatePluginOutput([]byte(`{}`), output); err != nil {
			t.Errorf("valid output %q rejected: %v", output, err)
		}
	}
	for _, output := range []string{`{invalid}`, "", `{"key":1,"key":2}`, `{} {}`} {
		if err := validatePluginOutput([]byte(`{}`), output); err == nil {
			t.Errorf("invalid output %q accepted", output)
		}
	}
	if err := validatePluginOutput(nil, "plain text"); err != nil {
		t.Fatalf("text without schema rejected: %v", err)
	}
}

func TestValidateAgainstSchemaType(t *testing.T) {
	schema := json.RawMessage(`{"type": "object"}`)
	if err := validatePluginOutput(schema, `{"k": "v"}`); err != nil {
		t.Errorf("object should pass: %v", err)
	}
	if err := validatePluginOutput(schema, `[1,2]`); err == nil {
		t.Error("array should fail object type check")
	}

	schemaArr := json.RawMessage(`{"type": "array"}`)
	if err := validatePluginOutput(schemaArr, `[1,2]`); err != nil {
		t.Errorf("array should pass: %v", err)
	}

	schemaStr := json.RawMessage(`{"type": "string"}`)
	if err := validatePluginOutput(schemaStr, `"hello"`); err != nil {
		t.Errorf("string should pass: %v", err)
	}

	schemaNum := json.RawMessage(`{"type": "number"}`)
	if err := validatePluginOutput(schemaNum, `42`); err != nil {
		t.Errorf("number should pass: %v", err)
	}

	schemaBool := json.RawMessage(`{"type": "boolean"}`)
	if err := validatePluginOutput(schemaBool, `true`); err != nil {
		t.Errorf("boolean should pass: %v", err)
	}
}

func TestValidateAgainstSchemaRequired(t *testing.T) {
	schema := json.RawMessage(`{"type": "object", "required": ["name", "age"]}`)
	if err := validatePluginOutput(schema, `{"name": "test", "age": 30}`); err != nil {
		t.Errorf("all required fields present: %v", err)
	}
	if err := validatePluginOutput(schema, `{"name": "test"}`); err == nil {
		t.Error("missing required field should fail")
	}
	if err := validatePluginOutput(schema, `"not an object"`); err == nil {
		t.Error("non-object should fail required field check")
	}
}

func TestValidateAgainstSchemaInvalidSchema(t *testing.T) {
	schema := json.RawMessage(`{invalid schema}`)
	if err := validatePluginOutput(schema, `{}`); err == nil {
		t.Error("invalid schema should return error")
	}
}
