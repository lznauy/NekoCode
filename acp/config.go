package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"time"

	controlruntime "nekocode/runtime"
)

// Session config option ids exposed over ACP.
const (
	configIDModel      = "model"
	configIDEffort     = "reasoning_effort"
	configIDFullAccess = "full_access"
)

type sessionConfig struct {
	Model           string
	ReasoningEffort string
	FullAccess      bool
}

// setConfigRequest mirrors SetSessionConfigOptionRequest. The value lives at
// the top level: a plain string for select options, or {"type":"boolean",
// "value":bool} for boolean options.
type setConfigRequest struct {
	SessionID string          `json:"sessionId"`
	ConfigID  string          `json:"configId"`
	Type      string          `json:"type"`
	Value     json.RawMessage `json:"value"`
}

func (r setConfigRequest) boolValue() (bool, bool) {
	if r.Type != "boolean" {
		return false, false
	}
	var value bool
	if err := json.Unmarshal(r.Value, &value); err != nil {
		return false, false
	}
	return value, true
}

func (r setConfigRequest) stringValue() (string, bool) {
	var value string
	if err := json.Unmarshal(r.Value, &value); err != nil || value == "" {
		return "", false
	}
	return value, true
}

// fullAccessValue accepts both wire forms: the boolean variant
// ({"type":"boolean","value":true}) and the select value ids "manual"/"full"
// used with clients that did not advertise boolean config support.
func (r setConfigRequest) fullAccessValue() (bool, *wireError) {
	if on, ok := r.boolValue(); ok {
		return on, nil
	}
	mode, ok := r.stringValue()
	if !ok {
		return false, rpcError(-32602, "full_access option requires a boolean or mode value")
	}
	switch mode {
	case "manual":
		return false, nil
	case "full":
		return true, nil
	default:
		return false, rpcError(-32602, "unknown permission mode %q", mode)
	}
}

// setConfigOption handles session/set_config_option: model selection,
// reasoning effort and the full-takeover permission mode.
func (s *server) setConfigOption(params json.RawMessage) (any, *wireError) {
	var request setConfigRequest
	if err := json.Unmarshal(params, &request); err != nil ||
		request.SessionID == "" || request.ConfigID == "" || len(request.Value) == 0 {
		return nil, rpcError(-32602, "invalid session/set_config_option params")
	}
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	if s.sessionConfigs == nil {
		s.sessionConfigs = make(map[string]sessionConfig)
	}
	if s.hasActiveTurn() {
		return nil, rpcError(-32000, "cannot change config while a prompt is running")
	}
	if !s.verifySession(request.SessionID) {
		return nil, rpcError(-32602, "unknown session %q", request.SessionID)
	}
	if s.backend.CurrentSessionID() != request.SessionID {
		return nil, rpcError(-32602, "session %q is not active", request.SessionID)
	}

	switch request.ConfigID {
	case configIDModel:
		name, ok := request.stringValue()
		if !ok {
			return nil, rpcError(-32602, "model option requires a string value")
		}
		models, _ := s.backend.ModelOptions()
		if !containsModel(models, name) {
			return nil, rpcError(-32602, "unknown model %q", name)
		}
		config := s.configForSession(request.SessionID)
		config.Model = name
		config.ReasoningEffort = s.defaultEffortFor(models, name)
		if err := s.applyBackendConfig(config); err != nil {
			return nil, backendError("switch model", err)
		}
		s.sessionConfigs[request.SessionID] = config
	case configIDEffort:
		effort, ok := request.stringValue()
		if !ok {
			return nil, rpcError(-32602, "reasoning_effort option requires a string value")
		}
		if !s.knownEffort(effort) {
			return nil, rpcError(-32602, "unsupported reasoning effort %q", effort)
		}
		if err := s.backend.SetSessionReasoning(effort); err != nil {
			return nil, backendError("set reasoning effort", err)
		}
		config := s.configForSession(request.SessionID)
		config.ReasoningEffort = s.backend.CurrentModel().ReasoningEffort
		s.sessionConfigs[request.SessionID] = config
	case configIDFullAccess:
		on, rpcErr := request.fullAccessValue()
		if rpcErr != nil {
			return nil, rpcErr
		}
		if err := s.backend.SetFullAccess(on); err != nil {
			return nil, backendError("set full access", err)
		}
		config := s.configForSession(request.SessionID)
		config.FullAccess = on
		s.sessionConfigs[request.SessionID] = config
	default:
		return nil, rpcError(-32602, "unknown config option %q", request.ConfigID)
	}
	return map[string]any{"configOptions": s.configOptions()}, nil
}

func (s *server) currentSessionConfig() sessionConfig {
	_, active := s.backend.ModelOptions()
	return sessionConfig{
		Model: active, ReasoningEffort: s.backend.CurrentModel().ReasoningEffort,
		FullAccess: s.backend.PermissionMode() == "full",
	}
}

// configForSession returns the connection-local config for a session. New or
// previously persisted sessions start from the process defaults captured when
// the ACP connection was created.
func (s *server) configForSession(sessionID string) sessionConfig {
	if config, ok := s.sessionConfigs[sessionID]; ok {
		return config
	}
	return s.defaultConfig
}

func (s *server) applySessionConfig(sessionID string) error {
	if s.sessionConfigs == nil {
		s.sessionConfigs = make(map[string]sessionConfig)
	}
	config := s.configForSession(sessionID)
	if err := s.applyBackendConfig(config); err != nil {
		return err
	}
	s.sessionConfigs[sessionID] = config
	return nil
}

func (s *server) applyBackendConfig(config sessionConfig) error {
	return s.backend.ApplySessionSettings(controlruntime.SessionSettings{
		Model: config.Model, ReasoningEffort: config.ReasoningEffort, FullAccess: config.FullAccess,
	})
}

func (s *server) cleanup() error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var cleanupErr error
	if err := s.backend.ReplaceMCPServers(ctx, mcpSource, nil); err != nil {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("clear session MCP servers: %w", err))
	}
	names := make([]string, 0, len(s.defaultEfforts))
	for name := range s.defaultEfforts {
		if name != s.defaultConfig.Model {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if _, err := s.backend.SwitchSessionModel(name); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("restore model %s: %w", name, err))
			continue
		}
		if err := s.backend.SetSessionReasoning(s.defaultEfforts[name]); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("restore reasoning effort for %s: %w", name, err))
		}
	}
	if err := s.applyBackendConfig(s.defaultConfig); err != nil {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("restore session config: %w", err))
	}
	return cleanupErr
}

// configOptions renders the full session config surface. The reasoning effort
// option is omitted when the active model has no controllable reasoning.
func (s *server) configOptions() []map[string]any {
	models, active := s.backend.ModelOptions()
	options := []map[string]any{modelOption(models, active)}
	if effort := effortOption(models, active, s.backend.CurrentModel().ReasoningEffort); effort != nil {
		options = append(options, effort)
	}
	return append(options, s.fullAccessOption())
}

func modelOption(models []controlruntime.ModelOption, active string) map[string]any {
	values := make([]map[string]any, 0, len(models))
	for _, model := range models {
		entry := map[string]any{"value": model.Name, "name": model.Name}
		if model.Model != "" && model.Model != model.Name {
			entry["description"] = model.Model
		}
		values = append(values, entry)
	}
	return map[string]any{
		"id":           configIDModel,
		"name":         "Model",
		"description":  "Active model configuration",
		"type":         "select",
		"currentValue": active,
		"options":      values,
	}
}

// effortOption returns nil when the active model has no controllable
// reasoning. "auto" maps to the empty stored effort and is always accepted.
func effortOption(models []controlruntime.ModelOption, active, currentEffort string) map[string]any {
	efforts := effortsFor(models, active)
	if len(efforts) == 0 {
		return nil
	}
	values := make([]map[string]any, 0, len(efforts)+1)
	values = append(values, map[string]any{"value": "auto", "name": "Auto"})
	for _, effort := range efforts {
		values = append(values, map[string]any{"value": effort, "name": effort})
	}
	if currentEffort == "" {
		currentEffort = "auto"
	}
	return map[string]any{
		"id":           configIDEffort,
		"name":         "Reasoning effort",
		"description":  "Thinking depth of the active model",
		"type":         "select",
		"currentValue": currentEffort,
		"options":      values,
	}
}

// fullAccessOption renders the permission mode. Clients that advertised
// boolean config support get a boolean toggle; everyone else gets the
// universally supported select form with the manual/full mode ids.
func (s *server) fullAccessOption() map[string]any {
	mode := s.backend.PermissionMode()
	option := map[string]any{
		"id":          configIDFullAccess,
		"name":        "Full access",
		"description": "Run every tool call without approval prompts; deny rules still apply",
	}
	if s.supportsBooleanConfig() {
		option["type"] = "boolean"
		option["currentValue"] = mode == "full"
		return option
	}
	option["type"] = "select"
	option["currentValue"] = mode
	option["options"] = []map[string]any{
		{"value": "manual", "name": "Manual"},
		{"value": "full", "name": "Full access"},
	}
	return option
}

func (s *server) supportsBooleanConfig() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clientBooleanConfig
}

func (s *server) knownEffort(effort string) bool {
	if effort == "auto" {
		return true
	}
	models, active := s.backend.ModelOptions()
	for _, candidate := range effortsFor(models, active) {
		if candidate == effort {
			return true
		}
	}
	return false
}

func containsModel(models []controlruntime.ModelOption, name string) bool {
	for _, model := range models {
		if model.Name == name {
			return true
		}
	}
	return false
}

func effortsFor(models []controlruntime.ModelOption, active string) []string {
	for _, model := range models {
		if model.Name == active {
			return model.ReasoningEfforts
		}
	}
	return nil
}

func (s *server) defaultEffortFor(models []controlruntime.ModelOption, name string) string {
	if effort, ok := s.defaultEfforts[name]; ok {
		return effort
	}
	for _, model := range models {
		if model.Name == name {
			return model.ReasoningEffort
		}
	}
	return ""
}

// verifySession reports whether the session belongs to this agent workspace.
// It accepts the active session even before it is persisted (sessions are
// only written once they contain messages). Callers must hold sessionMu.
func (s *server) verifySession(sessionID string) bool {
	if s.backend.CurrentSessionID() == sessionID {
		return true
	}
	meta, ok := s.findSession(sessionID)
	return ok && filepath.Clean(meta.CWD) == s.cwd
}
