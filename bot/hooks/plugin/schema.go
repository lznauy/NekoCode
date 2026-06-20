package plugin

import (
	"encoding/json"
	"fmt"
)

func isValidJSON(s string) bool {
	var js json.RawMessage
	return json.Unmarshal([]byte(s), &js) == nil
}

func validateAgainstSchema(schema json.RawMessage, output string) error {
	var schemaObj any
	if err := json.Unmarshal(schema, &schemaObj); err != nil {
		return fmt.Errorf("invalid output_schema: %w", err)
	}

	var outputObj any
	if err := json.Unmarshal([]byte(output), &outputObj); err != nil {
		return fmt.Errorf("output is not valid JSON: %w", err)
	}

	schemaMap, ok := schemaObj.(map[string]any)
	if !ok {
		return nil
	}
	if expectedType, ok := schemaMap["type"].(string); ok {
		if !matchJSONType(expectedType, outputObj) {
			return fmt.Errorf("expected JSON type %q", expectedType)
		}
	}
	if required, ok := schemaMap["required"].([]any); ok {
		outputMap, ok := outputObj.(map[string]any)
		if !ok {
			return fmt.Errorf("expected object for required field check")
		}
		for _, r := range required {
			key, ok := r.(string)
			if !ok {
				continue
			}
			if _, exists := outputMap[key]; !exists {
				return fmt.Errorf("missing required field %q", key)
			}
		}
	}
	return nil
}

func matchJSONType(expected string, val any) bool {
	switch expected {
	case "object":
		_, ok := val.(map[string]any)
		return ok
	case "array":
		_, ok := val.([]any)
		return ok
	case "string":
		_, ok := val.(string)
		return ok
	case "number":
		_, ok := val.(float64)
		return ok
	case "boolean":
		_, ok := val.(bool)
		return ok
	case "null":
		return val == nil
	}
	return true
}
