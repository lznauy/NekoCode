package plugin

import (
	"encoding/json"
	"fmt"
)

// This is intentionally a small, explicit subset; unsupported constraints fail
// at load time instead of appearing to be enforced at runtime.
type outputSchema struct {
	Type     string   `json:"type,omitempty"`
	Required []string `json:"required,omitempty"`
}

func parseOutputSchema(raw []byte) (outputSchema, error) {
	var schema outputSchema
	var fields map[string]json.RawMessage
	if err := strictJSON(raw, &fields); err != nil || fields == nil {
		return schema, fmt.Errorf("schema must be an object")
	}
	if err := strictJSON(raw, &schema); err != nil {
		return schema, fmt.Errorf("only top-level type and required are supported: %w", err)
	}
	if value, ok := fields["type"]; ok && (schema.Type == "" || string(value) == "null") {
		return schema, fmt.Errorf("type must name a supported JSON type")
	}
	if value, ok := fields["required"]; ok && string(value) == "null" {
		return schema, fmt.Errorf("required must be an array of strings")
	}
	switch schema.Type {
	case "", "object", "array", "string", "number", "boolean", "null":
	default:
		return schema, fmt.Errorf("unsupported schema type %q", schema.Type)
	}
	if schema.Type != "" && schema.Type != "object" && len(schema.Required) > 0 {
		return schema, fmt.Errorf("required is only supported for objects")
	}
	seen := map[string]bool{}
	for _, key := range schema.Required {
		if key == "" || seen[key] {
			return schema, fmt.Errorf("required keys must be non-empty and unique")
		}
		seen[key] = true
	}
	return schema, nil
}

// Decode output once, then check schema constraints on the decoded value.
func validatePluginOutput(raw []byte, output string) error {
	if raw == nil {
		return nil
	}
	var value any
	if err := strictJSON([]byte(output), &value); err != nil {
		return fmt.Errorf("output is not valid JSON (output_schema specified); output rejected")
	}
	schema, err := parseOutputSchema(raw)
	if err == nil {
		err = schema.validate(value)
	}
	if err != nil {
		return fmt.Errorf("schema validation failed: %v; output rejected", err)
	}
	return nil
}

func (schema outputSchema) validate(value any) error {
	if schema.Type != "" && !matchJSONType(schema.Type, value) {
		return fmt.Errorf("expected JSON type %q", schema.Type)
	}
	if len(schema.Required) > 0 {
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("expected object for required field check")
		}
		for _, key := range schema.Required {
			if _, ok := object[key]; !ok {
				return fmt.Errorf("missing required field %q", key)
			}
		}
	}
	return nil
}

func matchJSONType(expected string, value any) bool {
	switch expected {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "number":
		_, ok := value.(float64)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "null":
		return value == nil
	}
	return false
}
