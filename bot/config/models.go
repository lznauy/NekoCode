package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"nekocode/protocol"
	"reflect"
	"strings"
	"unicode"
)

// ProviderConfig owns connection settings shared by its models.
type ProviderConfig struct {
	Name     string            `json:"name"`
	BaseURL  string            `json:"base_url,omitempty"`
	APIKey   string            `json:"api_key,omitempty"`
	Protocol string            `json:"protocol,omitempty"`
	Models   []ModelDefinition `json:"models"`
}

type ModelDefinition = protocol.ModelDefinition

func (c *Config) UnmarshalJSON(data []byte) error {
	if trimmed := bytes.TrimSpace(data); len(trimmed) == 0 || trimmed[0] != '{' {
		return fmt.Errorf("configuration must be a JSON object")
	}
	type plain Config
	next := plain(*c)
	fields, err := configFields(data)
	if err != nil {
		return err
	}
	if _, supplied := fields["providers"]; supplied {
		// Provider entries are complete connection definitions. JSON slice
		// decoding otherwise reuses previous elements and their omitted fields.
		next.Providers = nil
		if _, activeSupplied := fields["active"]; !activeSupplied {
			next.Active = ""
		}
	}
	if err := decodeConfigJSON(data, &next); err != nil {
		return err
	}
	*c = Config(next)
	return nil
}

// Match field names exactly before decoding: encoding/json otherwise accepts
// case variants and silently applies duplicate fields in sequence.
func configFields(data []byte) (map[string]json.RawMessage, error) {
	allowed := make(map[string]bool)
	typ := reflect.TypeOf(Config{})
	for i := 0; i < typ.NumField(); i++ {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		allowed[name] = true
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		name, ok := token.(string)
		if !ok || !allowed[name] {
			return nil, fmt.Errorf("unknown field %q", token)
		}
		if _, duplicate := fields[name]; duplicate {
			return nil, fmt.Errorf("duplicate configuration field %q", name)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		fields[name] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	return fields, nil
}

// Resolve returns an independent runtime configuration with a qualified name.
func (p ProviderConfig) Resolve(m ModelDefinition) ModelConfig {
	protocol := m.Protocol
	if protocol == "" {
		protocol = p.Protocol
	}
	var vision *bool
	if m.Vision != nil {
		value := *m.Vision
		vision = &value
	}
	return ModelConfig{
		Name: p.Name + "/" + m.LocalName(), Provider: p.Name,
		APIKey: p.APIKey, BaseURL: p.BaseURL, Protocol: protocol,
		Model: m.Model, ReasoningEffort: m.ReasoningEffort,
		ContextWindow: m.ContextWindow, Vision: vision,
	}
}

func (c Config) AllModels() []ModelConfig {
	var models []ModelConfig
	for _, provider := range c.Providers {
		for _, model := range provider.Models {
			models = append(models, provider.Resolve(model))
		}
	}
	return models
}

func validModelName(name string) bool {
	return name != "" && strings.IndexFunc(name, unicode.IsSpace) < 0
}

func validProtocol(protocol string) bool {
	return protocol == "" || protocol == "openai" || protocol == "anthropic"
}

func (c *Config) validateProviders() error {
	if len(c.Providers) == 0 {
		return fmt.Errorf("at least one provider with a model is required")
	}
	seen := map[string]bool{}
	for i := range c.Providers {
		p := &c.Providers[i]
		p.Name = strings.TrimSpace(p.Name)
		p.BaseURL = strings.TrimSpace(p.BaseURL)
		p.APIKey = strings.TrimSpace(p.APIKey)
		p.Protocol = strings.TrimSpace(p.Protocol)
		if !validModelName(p.Name) || strings.Contains(p.Name, "/") {
			return fmt.Errorf("provider #%d name must be nonempty and contain no whitespace or slash", i+1)
		}
		if seen[p.Name] {
			return fmt.Errorf("duplicate provider name %q", p.Name)
		}
		seen[p.Name] = true
		if !validProtocol(p.Protocol) {
			return fmt.Errorf("provider %q protocol must be openai or anthropic", p.Name)
		}
		if len(p.Models) == 0 {
			return fmt.Errorf("provider %q must contain at least one model", p.Name)
		}
		models := map[string]bool{}
		for j := range p.Models {
			m := &p.Models[j]
			m.Name = strings.TrimSpace(m.Name)
			m.Model = strings.TrimSpace(m.Model)
			m.Protocol = strings.TrimSpace(m.Protocol)
			m.ReasoningEffort = strings.ToLower(strings.TrimSpace(m.ReasoningEffort))
			if !validModelName(m.Model) {
				return fmt.Errorf("provider %q model #%d requires a model ID without whitespace", p.Name, j+1)
			}
			name := m.LocalName()
			if !validModelName(name) {
				return fmt.Errorf("provider %q model #%d has an invalid name", p.Name, j+1)
			}
			if models[name] {
				return fmt.Errorf("duplicate model name %q in provider %q", name, p.Name)
			}
			models[name] = true
			if !validProtocol(m.Protocol) {
				return fmt.Errorf("model %q protocol must be openai or anthropic", p.Name+"/"+name)
			}
			if m.ContextWindow < 0 {
				return fmt.Errorf("model %q context_window must not be negative", p.Name+"/"+name)
			}
			capability := ReasoningCapabilityFor(p.Resolve(*m))
			if !capability.Supports(m.ReasoningEffort) {
				return fmt.Errorf("model %q reasoning_effort must be one of %s", p.Name+"/"+name, strings.Join(capability.Values(), ", "))
			}
		}
	}
	return nil
}

func (c *Config) SetModelReasoning(name, effort string) bool {
	for i := range c.Providers {
		p := &c.Providers[i]
		for j := range p.Models {
			if p.Name+"/"+p.Models[j].LocalName() == name {
				p.Models[j].ReasoningEffort = effort
				return true
			}
		}
	}
	return false
}

func decodeConfigJSON(data []byte, target any) error {
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
