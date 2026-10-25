package plugin

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dop251/goja"

	"nekocode/bot/policy/contract"
)

const (
	maxPluginOutputBytes  = 4096
	pluginOutputUntrusted = "true"
)

type eventHook struct {
	ID      string       `json:"id,omitempty"`
	Mode    string       `json:"mode,omitempty"`
	Matcher string       `json:"matcher,omitempty"`
	Hooks   []hookAction `json:"hooks"`
}

type hookAction struct {
	Type         string          `json:"type"`
	Command      string          `json:"command,omitempty"`
	Code         string          `json:"code,omitempty"`
	Path         string          `json:"path,omitempty"`
	Timeout      int             `json:"timeout,omitempty"`
	OutputSchema json.RawMessage `json:"output_schema,omitempty"`
	OutputFormat string          `json:"output_format,omitempty"`
	program      *goja.Program
}

// Filtered aliases share canonical capabilities and do not add engine points.
type eventSpec struct {
	contract.PointSpec
	failureOnly bool
}

var eventSpecs = pluginEventSpecs()

func pluginEventSpecs() []eventSpec {
	var events []eventSpec
	for _, spec := range contract.Points() {
		events = append(events, eventSpec{PointSpec: spec})
		if spec.Point == PostToolUse {
			spec.Name = "PostToolUseFailure"
			events = append(events, eventSpec{PointSpec: spec, failureOnly: true})
		}
	}
	return events
}

func Load(pluginRoot, hooksPath string) ([]Hook, error) {
	path, err := resolvePluginPath(pluginRoot, hooksPath)
	if err != nil {
		return nil, err
	}
	data, err := readLimitedFile(path, maxPluginSourceBytes)
	if err != nil {
		return nil, fmt.Errorf("read hooks file: %w", err)
	}
	var raw map[string]json.RawMessage
	if err := strictJSON(data, &raw); err != nil {
		return nil, fmt.Errorf("parse hooks json: %w", err)
	}
	if raw == nil {
		return nil, fmt.Errorf("hooks config must be an object")
	}
	protocolVersion := 0
	if version, ok := raw["version"]; ok {
		var v int
		if err := json.Unmarshal(version, &v); err != nil || v != 1 {
			return nil, fmt.Errorf("unsupported hook protocol version")
		}
		protocolVersion = v
		delete(raw, "version")
	}
	known := map[string]bool{}
	for _, spec := range eventSpecs {
		known[spec.Name] = true
	}
	for name := range raw {
		if !known[name] {
			return nil, fmt.Errorf("unknown hook event %q", name)
		}
	}
	var hooks []Hook
	total := 0
	seen := map[string]bool{}
	occurrences := map[string]int{}
	for _, spec := range eventSpecs {
		value, ok := raw[spec.Name]
		if !ok {
			continue
		}
		var entries []eventHook
		if err := strictJSON(value, &entries); err != nil {
			return nil, fmt.Errorf("%s: %w", spec.Name, err)
		}
		for i, eh := range entries {
			total++
			if total > 256 {
				return nil, fmt.Errorf("plugin config exceeds 256 hook entries")
			}
			if err := validateEntry(pluginRoot, spec, eh); err != nil {
				return nil, fmt.Errorf("%s[%d]: %w", spec.Name, i, err)
			}
			id := eh.ID
			if id == "" {
				encoded, _ := json.Marshal(eh)
				digest := sha256.Sum256(encoded)
				key := fmt.Sprintf("%s:%x", spec.Name, digest[:8])
				occurrences[key]++
				id = fmt.Sprintf("%s:%d", key, occurrences[key])
			} else {
				id = spec.Name + ":" + id
			}
			if seen[id] {
				return nil, fmt.Errorf("duplicate hook id %q", id)
			}
			seen[id] = true
			for j := range eh.Hooks {
				if eh.Hooks[j].Type == "command" && eh.Hooks[j].OutputFormat == "" {
					eh.Hooks[j].OutputFormat = "text"
					if protocolVersion == 1 {
						eh.Hooks[j].OutputFormat = "json"
					}
				}
			}
			h := makePluginHook(spec.Point, spec.Name, pluginRoot, eh, spec.failureOnly)
			h.Name = "plugin:" + id
			on, name := h.On, h.Name
			h.On = func(e Event) *Result {
				if e.HookID == "" {
					e.HookID = name
				}
				return on(e)
			}
			h.SessionScoped = spec.SessionScoped
			h.Once = spec.Once
			h.Guard = eh.Mode == "guard" || (eh.Mode == "" && spec.DefaultGuard)
			var budget time.Duration
			for _, a := range eh.Hooks {
				timeout := a.Timeout
				if timeout == 0 {
					timeout = 5000
				}
				budget += time.Duration(timeout) * time.Millisecond
			}
			if budget > 60*time.Second {
				budget = 60 * time.Second
			}
			h.Timeout = budget
			hooks = append(hooks, h)
		}
	}
	return hooks, nil
}

func validateEntry(pluginRoot string, spec eventSpec, eh eventHook) error {
	if eh.Mode != "" && eh.Mode != "guard" && eh.Mode != "observer" {
		return fmt.Errorf("unknown mode %q", eh.Mode)
	}
	if eh.Mode == "guard" && !spec.CanReject {
		return fmt.Errorf("%s only supports observers", spec.Name)
	}
	if _, err := compileMatcher(eh.Matcher); err != nil {
		return fmt.Errorf("invalid matcher: %w", err)
	}
	if !spec.ToolMatcher && eh.Matcher != "" && eh.Matcher != ".*" {
		return fmt.Errorf("tool matcher is not supported at %s", spec.Name)
	}
	if len(eh.Hooks) == 0 || len(eh.Hooks) > 32 {
		return fmt.Errorf("hooks must contain 1..32 actions")
	}
	for i := range eh.Hooks {
		a := &eh.Hooks[i]
		if a.Timeout < 0 || a.Timeout > 60000 {
			return fmt.Errorf("action %d timeout must be 0..60000 milliseconds", i)
		}
		switch a.Type {
		case "command":
			if a.Command == "" || a.Code != "" || a.Path != "" {
				return fmt.Errorf("action %d requires only command", i)
			}
		case "js":
			if (a.Code == "") == (a.Path == "") || a.Command != "" {
				return fmt.Errorf("action %d requires exactly one of code or path", i)
			}
		default:
			return fmt.Errorf("action %d has unknown type %q", i, a.Type)
		}
		if a.Type == "js" {
			source, name, err := jsSource(pluginRoot, *a)
			if err != nil {
				return fmt.Errorf("action %d: %w", i, err)
			}
			program, err := goja.Compile(name, source, false)
			if err != nil {
				return fmt.Errorf("action %d: %w", i, err)
			}
			a.program = program
		}
		if a.OutputFormat != "" && (a.Type != "command" || (a.OutputFormat != "text" && a.OutputFormat != "json")) {
			return fmt.Errorf("action %d: output_format must be text or json on commands", i)
		}
		if a.OutputSchema != nil {
			if _, err := parseOutputSchema(a.OutputSchema); err != nil {
				return fmt.Errorf("action %d output_schema: %w", i, err)
			}
		}
	}
	return nil
}
