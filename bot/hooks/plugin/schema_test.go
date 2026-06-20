package plugin

import (
	"encoding/json"
	"testing"
)

func TestIsValidJSON(t *testing.T) {
	if !isValidJSON(`{"key": "value"}`) {
		t.Error("object should be valid JSON")
	}
	if !isValidJSON(`[1, 2, 3]`) {
		t.Error("array should be valid JSON")
	}
	if !isValidJSON(`"hello"`) {
		t.Error("string should be valid JSON")
	}
	if !isValidJSON(`42`) {
		t.Error("number should be valid JSON")
	}
	if isValidJSON(`{invalid}`) {
		t.Error("invalid syntax should not be valid JSON")
	}
	if isValidJSON("") {
		t.Error("empty should not be valid JSON")
	}
}

func TestValidateAgainstSchemaType(t *testing.T) {
	schema := json.RawMessage(`{"type": "object"}`)
	if err := validateAgainstSchema(schema, `{"k": "v"}`); err != nil {
		t.Errorf("object should pass: %v", err)
	}
	if err := validateAgainstSchema(schema, `[1,2]`); err == nil {
		t.Error("array should fail object type check")
	}

	schemaArr := json.RawMessage(`{"type": "array"}`)
	if err := validateAgainstSchema(schemaArr, `[1,2]`); err != nil {
		t.Errorf("array should pass: %v", err)
	}

	schemaStr := json.RawMessage(`{"type": "string"}`)
	if err := validateAgainstSchema(schemaStr, `"hello"`); err != nil {
		t.Errorf("string should pass: %v", err)
	}

	schemaNum := json.RawMessage(`{"type": "number"}`)
	if err := validateAgainstSchema(schemaNum, `42`); err != nil {
		t.Errorf("number should pass: %v", err)
	}

	schemaBool := json.RawMessage(`{"type": "boolean"}`)
	if err := validateAgainstSchema(schemaBool, `true`); err != nil {
		t.Errorf("boolean should pass: %v", err)
	}
}

func TestValidateAgainstSchemaRequired(t *testing.T) {
	schema := json.RawMessage(`{"type": "object", "required": ["name", "age"]}`)
	if err := validateAgainstSchema(schema, `{"name": "test", "age": 30}`); err != nil {
		t.Errorf("all required fields present: %v", err)
	}
	if err := validateAgainstSchema(schema, `{"name": "test"}`); err == nil {
		t.Error("missing required field should fail")
	}
	if err := validateAgainstSchema(schema, `"not an object"`); err == nil {
		t.Error("non-object should fail required field check")
	}
}

func TestValidateAgainstSchemaInvalidSchema(t *testing.T) {
	schema := json.RawMessage(`{invalid schema}`)
	if err := validateAgainstSchema(schema, `{}`); err == nil {
		t.Error("invalid schema should return error")
	}
}
