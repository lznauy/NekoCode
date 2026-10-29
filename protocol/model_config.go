package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// ModelDefinition is the persisted model entry. A plain model ID is shorthand
// for an object with only Model set. Connection credentials stay on the provider.
type ModelDefinition struct {
	Name            string `json:"name,omitempty"`
	Model           string `json:"model"`
	Protocol        string `json:"protocol,omitempty"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	ContextWindow   int    `json:"context_window,omitempty"`
	Vision          *bool  `json:"vision,omitempty"`
	object          bool
}

func decodeModelJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("expected exactly one JSON value")
	}
	return nil
}

func (m *ModelDefinition) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) > 0 && data[0] == '"' {
		var id string
		if err := json.Unmarshal(data, &id); err != nil {
			return err
		}
		*m = ModelDefinition{Model: id}
		return nil
	}
	if len(data) == 0 || data[0] != '{' {
		return fmt.Errorf("model entry must be a model ID string or an object")
	}
	type plain ModelDefinition
	var next plain
	if err := decodeModelJSON(data, &next); err != nil {
		return err
	}
	*m = ModelDefinition(next)
	m.object = true
	return nil
}

func (m ModelDefinition) MarshalJSON() ([]byte, error) {
	if !m.object && m.Name == "" && m.Protocol == "" && m.ReasoningEffort == "" && m.ContextWindow == 0 && m.Vision == nil {
		return json.Marshal(m.Model)
	}
	type plain ModelDefinition
	return json.Marshal(plain(m))
}

func (m ModelDefinition) LocalName() string {
	if m.Name != "" {
		return m.Name
	}
	return m.Model
}
