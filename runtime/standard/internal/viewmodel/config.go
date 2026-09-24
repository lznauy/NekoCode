package viewmodel

import (
	"nekocode/bot/config"
	controlruntime "nekocode/runtime"
)

func jevConfigToView(in *config.JevConfig) *controlruntime.JevConfig {
	if in == nil {
		return nil
	}
	out := controlruntime.JevConfig(*in)
	return &out
}

func jevConfigFromView(in *controlruntime.JevConfig) *config.JevConfig {
	if in == nil {
		return nil
	}
	out := config.JevConfig(*in)
	return &out
}

func modelConfigsToView(in []config.ModelConfig) []controlruntime.ModelConfig {
	if in == nil {
		return nil
	}
	out := make([]controlruntime.ModelConfig, 0, len(in))
	for _, m := range in {
		out = append(out, modelConfigToView(m))
	}
	return out
}

func modelConfigsFromView(in []controlruntime.ModelConfig) []config.ModelConfig {
	if in == nil {
		return nil
	}
	out := make([]config.ModelConfig, 0, len(in))
	for _, m := range in {
		out = append(out, modelConfigFromView(m))
	}
	return out
}

func modelConfigToView(m config.ModelConfig) controlruntime.ModelConfig {
	return controlruntime.ModelConfig{
		Name: m.Name, Provider: m.Provider, APIKey: m.APIKey, Model: m.Model,
		BaseURL: m.BaseURL, Protocol: m.Protocol, ReasoningEffort: m.ReasoningEffort,
		ContextWindow: m.ContextWindow, Profile: ModelProfile(m),
	}
}

func modelConfigFromView(m controlruntime.ModelConfig) config.ModelConfig {
	return config.ModelConfig{
		Name: m.Name, Provider: m.Provider, APIKey: m.APIKey, Model: m.Model,
		BaseURL: m.BaseURL, Protocol: m.Protocol, ReasoningEffort: m.ReasoningEffort,
		ContextWindow: m.ContextWindow,
	}
}

func ModelProfile(m config.ModelConfig) controlruntime.ModelProfile {
	window, source := m.ContextWindow, "override"
	if window <= 0 {
		if known, ok := config.KnownContextWindow(m.Model); ok {
			window, source = known, "model"
		} else {
			window = config.DefaultContextWindow
			source = "default"
		}
	}
	return controlruntime.ModelProfile{
		ContextWindow: window, ContextWindowSource: source,
		ReasoningEfforts: config.ReasoningCapabilityFor(m).Efforts,
	}
}

func ModelProfileFromSpec(spec controlruntime.ModelSpec) controlruntime.ModelProfile {
	return ModelProfile(config.ModelConfig{
		Provider: spec.Provider, Model: spec.Model, Protocol: spec.Protocol, ContextWindow: spec.ContextWindow,
	})
}

func imageGenConfigsToView(in []config.ImageGenConfig) []controlruntime.ImageGenConfig {
	if in == nil {
		return nil
	}
	out := make([]controlruntime.ImageGenConfig, 0, len(in))
	for _, m := range in {
		out = append(out, controlruntime.ImageGenConfig{
			Name:      m.Name,
			Provider:  m.Provider,
			APIKey:    m.APIKey,
			SecretKey: m.SecretKey,
			BaseURL:   m.BaseURL,
			Model:     m.Model,
		})
	}
	return out
}

func imageGenConfigsFromView(in []controlruntime.ImageGenConfig) []config.ImageGenConfig {
	if in == nil {
		return nil
	}
	out := make([]config.ImageGenConfig, 0, len(in))
	for _, m := range in {
		out = append(out, config.ImageGenConfig{
			Name:      m.Name,
			Provider:  m.Provider,
			APIKey:    m.APIKey,
			SecretKey: m.SecretKey,
			BaseURL:   m.BaseURL,
			Model:     m.Model,
		})
	}
	return out
}

func mcpServerConfigsToView(in map[string]config.MCPServerConfig) map[string]controlruntime.MCPServerConfig {
	if in == nil {
		return nil
	}
	out := make(map[string]controlruntime.MCPServerConfig, len(in))
	for name, srv := range in {
		view := controlruntime.MCPServerConfig(srv)
		view.Args = append([]string(nil), srv.Args...)
		view.Env = stringMap(srv.Env)
		out[name] = view
	}
	return out
}

func mcpServerConfigsFromView(in map[string]controlruntime.MCPServerConfig) map[string]config.MCPServerConfig {
	if in == nil {
		return nil
	}
	out := make(map[string]config.MCPServerConfig, len(in))
	for name, srv := range in {
		cfg := config.MCPServerConfig(srv)
		cfg.Args = append([]string(nil), srv.Args...)
		cfg.Env = stringMap(srv.Env)
		out[name] = cfg
	}
	return out
}

func permissionsConfigToView(in *config.PermissionsConfig) *controlruntime.PermissionsConfig {
	if in == nil {
		return nil
	}
	return &controlruntime.PermissionsConfig{
		Allow:   append([]string(nil), in.Allow...),
		Ask:     append([]string(nil), in.Ask...),
		Deny:    append([]string(nil), in.Deny...),
		Sandbox: sandboxConfigsToView(in.Sandbox),
	}
}

func permissionsConfigFromView(in *controlruntime.PermissionsConfig) *config.PermissionsConfig {
	if in == nil {
		return nil
	}
	return &config.PermissionsConfig{
		Allow:   append([]string(nil), in.Allow...),
		Ask:     append([]string(nil), in.Ask...),
		Deny:    append([]string(nil), in.Deny...),
		Sandbox: sandboxConfigsFromView(in.Sandbox),
	}
}

func sandboxConfigsToView(in map[string]config.SandboxConfig) map[string]controlruntime.SandboxConfig {
	if in == nil {
		return nil
	}
	out := make(map[string]controlruntime.SandboxConfig, len(in))
	for name, sandbox := range in {
		out[name] = controlruntime.SandboxConfig{
			SandboxMode:   sandbox.SandboxMode,
			Network:       sandbox.Network,
			WritableRoots: append([]string(nil), sandbox.WritableRoots...),
		}
	}
	return out
}

func sandboxConfigsFromView(in map[string]controlruntime.SandboxConfig) map[string]config.SandboxConfig {
	if in == nil {
		return nil
	}
	out := make(map[string]config.SandboxConfig, len(in))
	for name, sandbox := range in {
		out[name] = config.SandboxConfig{
			SandboxMode:   sandbox.SandboxMode,
			Network:       sandbox.Network,
			WritableRoots: append([]string(nil), sandbox.WritableRoots...),
		}
	}
	return out
}

func workspaceConfigsToView(in []config.WorkspaceConfig) []controlruntime.WorkspaceConfig {
	if in == nil {
		return nil
	}
	out := make([]controlruntime.WorkspaceConfig, 0, len(in))
	for _, w := range in {
		out = append(out, controlruntime.WorkspaceConfig{Path: w.Path, Access: w.Access})
	}
	return out
}

func workspaceConfigsFromView(in []controlruntime.WorkspaceConfig) []config.WorkspaceConfig {
	if in == nil {
		return nil
	}
	out := make([]config.WorkspaceConfig, 0, len(in))
	for _, w := range in {
		out = append(out, config.WorkspaceConfig{Path: w.Path, Access: w.Access})
	}
	return out
}

func stringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
