package standard

import (
	"context"

	"nekocode/bot/config"
	"nekocode/bot/core"
	"nekocode/bot/extension"
	"nekocode/bot/extension/mcp"
	controlruntime "nekocode/runtime"
	"nekocode/runtime/standard/internal/viewmodel"
)

// adapter is the only standard-application boundary between the bot domain
// and runtime's interaction/read-model protocol.
type adapter struct {
	bot *core.Bot
}

func adapt(standardBot *core.Bot) *adapter {
	return &adapter{bot: standardBot}
}

func (a *adapter) Run(ctx context.Context, input string, host controlruntime.RunHost) (string, error) {
	return a.bot.Run(ctx, input, host)
}

func (a *adapter) ExecuteCommand(ctx context.Context, input string, host controlruntime.RunHost) (controlruntime.CommandResult, error) {
	return a.bot.ExecuteCommand(ctx, input, host)
}

func (a *adapter) ExecuteLocalCommand(ctx context.Context, input string) (string, controlruntime.LocalCommandResult) {
	return a.bot.ExecuteLocalCommand(ctx, input)
}

func (a *adapter) CommandMenu(ctx context.Context, input string) (controlruntime.CommandMenu, bool) {
	return a.bot.CommandMenu(ctx, input)
}

func (a *adapter) Steer(ctx context.Context, message string) error {
	return a.bot.Steer(ctx, message)
}

func (a *adapter) Metrics() controlruntime.MetricsSnapshot {
	return a.bot.Metrics()
}

func (a *adapter) CurrentModel() controlruntime.ModelSelection {
	config := a.bot.Configuration()
	return viewmodel.Model(config.ActiveModelConfig())
}

// PermissionMode reports the permission mode: "full" when the full-takeover
// mode is active, "manual" otherwise.
func (a *adapter) PermissionMode() string {
	if a.bot.FullAccess() {
		return "full"
	}
	return "manual"
}

func (a *adapter) SwitchModel(name string) (controlruntime.ModelSelection, error) {
	if err := a.bot.SwitchModel(name); err != nil {
		return controlruntime.ModelSelection{}, err
	}
	return a.CurrentModel(), nil
}

func (a *adapter) SwitchSessionModel(name string) (controlruntime.ModelSelection, error) {
	if err := a.bot.SwitchModelRuntime(name); err != nil {
		return controlruntime.ModelSelection{}, err
	}
	return a.CurrentModel(), nil
}

// ModelOptions projects the configured models and the active one for session
// config surfaces. API keys and other secrets stay in the config layer.
func (a *adapter) ModelOptions() ([]controlruntime.ModelOption, string) {
	configuration := a.bot.Configuration()
	options := make([]controlruntime.ModelOption, 0, len(configuration.Models))
	for _, model := range configuration.Models {
		options = append(options, controlruntime.ModelOption{
			Name:             model.Name,
			Model:            model.Model,
			ReasoningEffort:  model.ReasoningEffort,
			ReasoningEfforts: config.ReasoningCapabilityFor(model).Efforts,
		})
	}
	return options, configuration.Active
}

func (a *adapter) SetReasoningEffort(effort string) error {
	return a.bot.SetReasoningEffort(effort)
}

func (a *adapter) SetSessionReasoning(effort string) error {
	return a.bot.SetReasoningEffortRuntime(effort)
}

func (a *adapter) SetFullAccess(on bool) {
	a.bot.SetFullAccess(on)
}

func (a *adapter) ContextSnapshot() controlruntime.ContextSnapshot {
	return viewmodel.ContextSnapshot(a.bot.ContextReport())
}

func (a *adapter) WorkspaceChanges() controlruntime.WorkspaceChanges {
	return a.bot.WorkspaceChanges()
}

func (a *adapter) MemoryView(scope controlruntime.MemoryScope) controlruntime.MemoryView {
	memory := a.bot.Memory()
	return viewmodel.Memory(scope, memory.Path, memory.Content)
}

func (a *adapter) SkillManagementView() controlruntime.SkillManagementView {
	return a.extensionView(a.bot.Extensions())
}

func (a *adapter) SelectSkill(name string) error {
	return a.bot.SelectSkill(name)
}

func (a *adapter) ClearSelectedSkill() {
	a.bot.ClearSelectedSkill()
}

func (a *adapter) RefreshSkillManagement() controlruntime.SkillManagementView {
	a.bot.RefreshExtensions()
	return a.SkillManagementView()
}

func (a *adapter) SetPluginEnabled(name string, enabled bool) (controlruntime.SkillManagementView, error) {
	if err := a.bot.SetPluginEnabled(name, enabled); err != nil {
		return controlruntime.SkillManagementView{}, err
	}
	return a.SkillManagementView(), nil
}

func (a *adapter) extensionView(snapshot extension.Snapshot) controlruntime.SkillManagementView {
	return viewmodel.Extension(snapshot)
}

func (a *adapter) ConfigView() controlruntime.ConfigView {
	return viewmodel.Config(a.bot.Configuration())
}

func (a *adapter) ResolveModelProfile(model controlruntime.ModelSpec) controlruntime.ModelProfile {
	return viewmodel.ModelProfileFromSpec(model)
}

func (a *adapter) ApplyConfig(view controlruntime.ConfigView) (controlruntime.ConfigView, error) {
	if err := a.bot.ApplyConfiguration(viewmodel.ToConfig(view)); err != nil {
		return controlruntime.ConfigView{}, err
	}
	return a.ConfigView(), nil
}

func (a *adapter) CurrentSessionID() string {
	return a.bot.CurrentSessionID()
}

func (a *adapter) ListSessions() []controlruntime.SessionMeta {
	return viewmodel.SessionMetas(a.bot.ListSessions())
}

func (a *adapter) SessionMessages() []controlruntime.DisplayMessage {
	snapshot := a.bot.Conversation()
	return viewmodel.DisplayMessages(snapshot.Transcript)
}

func (a *adapter) ResumeSession(id string) error {
	return a.bot.ResumeSession(id)
}

func (a *adapter) NewSession() (controlruntime.SessionMeta, error) {
	snapshot, err := a.bot.NewSession()
	if err != nil {
		return controlruntime.SessionMeta{}, err
	}
	return viewmodel.SessionSnapshot(snapshot), nil
}

func (a *adapter) DeleteSession(id string) error {
	return a.bot.DeleteSession(id)
}

func (a *adapter) ReplaceMCPServers(ctx context.Context, source string, servers []controlruntime.MCPServerSpec) error {
	configs := make(map[string]mcp.ServerConfig, len(servers))
	for _, server := range servers {
		configs[server.Name] = mcp.ServerConfig{
			Command: server.Config.Command,
			Args:    append([]string(nil), server.Config.Args...),
			Env:     server.Config.Env,
		}
	}
	return a.bot.ReplaceSessionMCPServers(ctx, source, configs)
}

func (a *adapter) Close() error {
	return a.bot.Close()
}

func (a *adapter) services() controlruntime.Services {
	return controlruntime.Services{
		ToolNames: a.bot.ToolNames, Rewind: a.bot.Rewind, Checkpoints: a.bot.Checkpoints,
		ExecuteCommand:         a.ExecuteCommand,
		ExecuteLocalCommand:    a.ExecuteLocalCommand,
		CommandMenu:            a.CommandMenu,
		Steer:                  a.Steer,
		Metrics:                a.Metrics,
		CurrentModel:           a.CurrentModel,
		PermissionMode:         a.PermissionMode,
		SwitchModel:            a.SwitchModel,
		SwitchSessionModel:     a.SwitchSessionModel,
		ModelOptions:           a.ModelOptions,
		SetReasoningEffort:     a.SetReasoningEffort,
		SetSessionReasoning:    a.SetSessionReasoning,
		SetFullAccess:          a.SetFullAccess,
		ContextSnapshot:        a.ContextSnapshot,
		WorkspaceChanges:       a.WorkspaceChanges,
		MemoryView:             a.MemoryView,
		SkillManagementView:    a.SkillManagementView,
		SelectSkill:            a.SelectSkill,
		ClearSelectedSkill:     a.ClearSelectedSkill,
		RefreshSkillManagement: a.RefreshSkillManagement,
		SetPluginEnabled:       a.SetPluginEnabled,
		ConfigView:             a.ConfigView,
		ResolveModelProfile:    a.ResolveModelProfile,
		ApplyConfig:            a.ApplyConfig,
		CurrentSessionID:       a.CurrentSessionID,
		ListSessions:           a.ListSessions,
		SessionMessages:        a.SessionMessages,
		ResumeSession:          a.ResumeSession,
		NewSession:             a.NewSession,
		DeleteSession:          a.DeleteSession,
		ReplaceMCPServers:      a.ReplaceMCPServers,
		Close:                  a.Close,
	}
}

var _ controlruntime.Runner = (*adapter)(nil)
