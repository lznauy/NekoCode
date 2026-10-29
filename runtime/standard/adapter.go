package standard

import (
	"context"
	"maps"
	"slices"
	"unicode/utf8"

	"nekocode/bot/config"
	"nekocode/bot/core"
	"nekocode/bot/extension/mcp"
	"nekocode/bot/provider/types"
	"nekocode/logger"
	controlruntime "nekocode/runtime"
	"nekocode/runtime/standard/internal/viewmodel"
	"nekocode/util/attachment"
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
	return a.bot.Run(ctx, input, messageImages(host), host)
}

// messageImages retrieves this run's attachments from the runtime host and
// converts them into message metadata for native delivery. The host is
// shared with the bot for the duration of the run, so reading it here is
// race-free. Attachment files are probed once; failures degrade to
// headerless entries that providers sniff themselves.
func messageImages(host controlruntime.RunHost) []types.MessageImage {
	source, ok := host.(controlruntime.RunImageSource)
	if !ok {
		return nil
	}
	attachments := source.RunImages()
	if len(attachments) == 0 {
		return nil
	}
	images := make([]types.MessageImage, 0, len(attachments))
	for _, item := range attachments {
		image := types.MessageImage{Path: item.Path}
		if mime, width, height, err := attachment.InspectImage(item.Path); err == nil {
			image.MIME, image.Width, image.Height = mime, width, height
		} else {
			logger.Log("vision: attachment %s could not be inspected (%v); provider will sniff its header", item.Path, err)
		}
		images = append(images, image)
	}
	return images
}

func (a *adapter) ExecuteCommand(ctx context.Context, input string, host controlruntime.RunHost) (controlruntime.CommandResult, error) {
	return a.bot.ExecuteCommand(ctx, input, host)
}

func (a *adapter) CurrentModel() controlruntime.ModelSelection {
	config := a.bot.Configuration()
	return viewmodel.Model(config.ActiveModelConfig())
}

// PermissionMode reports the permission mode: "full" when the full-takeover
// mode is active, "auto" when the Jev-judged bash auto mode is active, and
// "manual" otherwise.
func (a *adapter) PermissionMode() string {
	if a.bot.FullAccess() {
		return "full"
	}
	if a.bot.BashAuto() {
		return "auto"
	}
	return "manual"
}

func (a *adapter) ImageAttachmentsEnabled() bool {
	// Pasted images are consumable when the active model accepts native image
	// input, or when an image-understanding model is configured for the tool
	// proxy path.
	configuration := a.bot.Configuration()
	if configuration.ActiveModelConfig().EffectiveVision() {
		return true
	}
	return len(configuration.ImageUnderstandModels) > 0
}

// VisionEnabled reports whether the active model accepts native image input.
func (a *adapter) VisionEnabled() bool {
	configuration := a.bot.Configuration()
	return configuration.ActiveModelConfig().EffectiveVision()
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
	options := make([]controlruntime.ModelOption, 0, len(configuration.AllModels()))
	for _, model := range configuration.AllModels() {
		options = append(options, controlruntime.ModelOption{
			Name:             model.Name,
			Model:            model.Model,
			ReasoningEffort:  model.ReasoningEffort,
			ReasoningEfforts: config.ReasoningCapabilityFor(model).Efforts,
		})
	}
	return options, configuration.Active
}

func (a *adapter) ContextSnapshot() controlruntime.ContextSnapshot {
	return viewmodel.ContextSnapshot(a.bot.ContextReport())
}

func (a *adapter) MemoryView(scope controlruntime.MemoryScope) controlruntime.MemoryView {
	memory := a.bot.Memory()
	return viewmodel.Memory(scope, memory.Path, memory.Content)
}

func (a *adapter) SkillManagementView() controlruntime.SkillManagementView {
	return viewmodel.Extension(a.bot.Extensions())
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

func (a *adapter) ConfigView() controlruntime.ConfigView {
	return viewmodel.Config(a.bot.Configuration())
}

func (a *adapter) ApplyConfig(view controlruntime.ConfigView) (controlruntime.ConfigView, error) {
	if err := a.bot.ApplyConfiguration(viewmodel.ToConfig(view)); err != nil {
		return controlruntime.ConfigView{}, err
	}
	return a.ConfigView(), nil
}

func (a *adapter) ListSessions() []controlruntime.SessionMeta {
	return viewmodel.SessionMetas(a.bot.ListSessions())
}

func (a *adapter) SessionMessages() []controlruntime.DisplayMessage {
	snapshot := a.bot.Conversation()
	return viewmodel.DisplayMessages(snapshot.Transcript)
}

func (a *adapter) SessionMessagesPage(offset, limit int) (controlruntime.SessionMessagePage, error) {
	snapshot := a.bot.Conversation()
	return viewmodel.DisplayMessagesPage(snapshot.Transcript, offset, limit)
}

func (a *adapter) SessionHistoryWindow(before, limit int) (controlruntime.SessionHistoryWindow, error) {
	raw, err := a.bot.HistoryPage(before, limit, 1<<20)
	if err != nil {
		return controlruntime.SessionHistoryWindow{}, err
	}
	// Bound display previews as well as page size. The complete text stays in
	// JSONL and remains available through streaming /export.
	for i := range raw.Messages {
		m := &raw.Messages[i]
		m.Content = historyPreview(m.Content)
		m.ReasoningContent = historyPreview(m.ReasoningContent)
		m.ToolCalls = slices.Clone(m.ToolCalls)
		for j := range m.ToolCalls {
			m.ToolCalls[j].Function.Arguments = historyPreview(m.ToolCalls[j].Function.Arguments)
		}
	}
	return controlruntime.SessionHistoryWindow{Messages: viewmodel.DisplayMessages(raw.Messages), Before: raw.Before, End: raw.End, HasMore: raw.HasMore, Total: raw.Total}, nil
}

func historyPreview(content string) string {
	if len(content) <= 64<<10 {
		return content
	}
	end := 64 << 10
	for end > 0 && !utf8.RuneStart(content[end]) {
		end--
	}
	return content[:end] + "\n… [显示预览；完整内容可用 /export 导出]"
}

func (a *adapter) NewSession() (controlruntime.SessionMeta, error) {
	snapshot, err := a.bot.NewSession()
	if snapshot == nil {
		return controlruntime.SessionMeta{}, err
	}
	return viewmodel.SessionSnapshot(snapshot), err
}

func (a *adapter) ReplaceMCPServers(ctx context.Context, source string, servers []controlruntime.MCPServerSpec) error {
	configs := sessionMCPConfigs(servers)
	return a.bot.ReplaceSessionMCPServers(ctx, source, configs)
}

func (a *adapter) services() controlruntime.ServiceGroups {
	return controlruntime.ServiceGroups{
		Commands: controlruntime.CommandsServices{
			ExecuteCommand:      a.ExecuteCommand,
			ExecuteLocalCommand: a.bot.ExecuteLocalCommand,
			CommandMenu:         a.bot.CommandMenu,
			Steer:               a.bot.Steer,
		},
		Models: controlruntime.ModelsServices{
			CurrentModel:            a.CurrentModel,
			SwitchModel:             a.SwitchModel,
			SwitchSessionModel:      a.SwitchSessionModel,
			ModelOptions:            a.ModelOptions,
			SetReasoningEffort:      a.bot.SetReasoningEffort,
			SetSessionReasoning:     a.bot.SetReasoningEffortRuntime,
			ImageAttachmentsEnabled: a.ImageAttachmentsEnabled,
			VisionEnabled:           a.VisionEnabled,
		},
		Permissions: controlruntime.PermissionsServices{
			PermissionMode: a.PermissionMode,
			SetFullAccess:  a.bot.SetFullAccess,
		},
		Context: controlruntime.ContextServices{
			ContextSnapshot:  a.ContextSnapshot,
			WorkspaceChanges: a.bot.WorkspaceChanges,
			MemoryView:       a.MemoryView,
		},
		Skills: controlruntime.SkillsServices{
			SkillManagementView:    a.SkillManagementView,
			SelectSkill:            a.bot.SelectSkill,
			ClearSelectedSkill:     a.bot.ClearSelectedSkill,
			RefreshSkillManagement: a.RefreshSkillManagement,
			SetPluginEnabled:       a.SetPluginEnabled,
		},
		Config: controlruntime.ConfigServices{
			ConfigView:          a.ConfigView,
			ResolveModelProfile: viewmodel.ModelProfileFromSpec,
			ApplyConfig:         a.ApplyConfig,
		},
		Sessions: controlruntime.SessionsServices{
			Subagents:           a.Subagents,
			SubagentAttempts:    a.SubagentAttempts,
			SubagentHistory:     a.SubagentHistory,
			SubagentToolOutput:  a.SubagentToolOutput,
			CurrentSessionID:    a.bot.CurrentSessionID,
			ListSessions:        a.ListSessions,
			SessionMessages:     a.SessionMessages,
			SessionMessagesPage: a.SessionMessagesPage,
			HistoryWindow:       a.SessionHistoryWindow,
			ResumeSession:       a.bot.ResumeSession,
			NewSession:          a.NewSession,
			DeleteSession:       a.bot.DeleteSession,
		},
		Tools: controlruntime.ToolsServices{
			Checkpoints: a.bot.Checkpoints,
			ToolNames:   a.bot.ToolNames,
			Rewind:      a.bot.Rewind,
		},
		MCP: controlruntime.MCPServices{
			MCPAuthorizationAction: a.bot.MCPAuthorizationAction,
			ReplaceMCPServers:      a.ReplaceMCPServers,
		},
		Lifecycle: controlruntime.LifecycleServices{
			Metrics: a.bot.Metrics,
			Close:   a.bot.Close,
		},
	}
}

var _ controlruntime.Runner = (*adapter)(nil)

func sessionMCPConfigs(servers []controlruntime.MCPServerSpec) map[string]mcp.ServerConfig {
	configs := make(map[string]mcp.ServerConfig, len(servers))
	for _, server := range servers {
		configs[server.Name] = mcp.ServerConfig{
			Command: server.Config.Command,
			URL:     server.Config.URL, OAuthClientID: server.Config.OAuthClientID, OAuthClientSecret: server.Config.OAuthClientSecret, OAuthClientMetadataURL: server.Config.OAuthClientMetadataURL, OAuthCallbackPort: server.Config.OAuthCallbackPort,
			Args: append([]string(nil), server.Config.Args...),
			Env:  maps.Clone(server.Config.Env),
		}
	}
	return configs
}
