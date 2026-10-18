package runtime

import "context"

// ServiceGroups is the canonical, domain-grouped application capability set.
// Runtime stores only this representation; New converts legacy Services once.
type ServiceGroups struct {
	Commands    CommandsServices
	Models      ModelsServices
	Permissions PermissionsServices
	Context     ContextServices
	Skills      SkillsServices
	Config      ConfigServices
	Sessions    SessionsServices
	Tools       ToolsServices
	MCP         MCPServices
	Lifecycle   LifecycleServices
}

// CommandsServices contains optional commands callbacks supplied by the application.
type CommandsServices struct {
	ExecuteCommand      func(ctx context.Context, input string, host RunHost) (CommandResult, error)
	ExecuteLocalCommand func(context.Context, string) (string, LocalCommandResult)
	CommandMenu         func(context.Context, string) (CommandMenu, bool)
	Steer               func(ctx context.Context, message string) error
}

// ModelsServices contains optional models callbacks supplied by the application.
type ModelsServices struct {
	CurrentModel            func() ModelSelection
	SwitchModel             func(string) (ModelSelection, error)
	SwitchSessionModel      func(string) (ModelSelection, error)
	ModelOptions            func() ([]ModelOption, string)
	SetReasoningEffort      func(string) error
	SetSessionReasoning     func(string) error
	ImageAttachmentsEnabled func() bool
	VisionEnabled           func() bool
}

// PermissionsServices contains optional permissions callbacks supplied by the application.
type PermissionsServices struct {
	PermissionMode func() string
	SetFullAccess  func(bool)
}

// ContextServices contains optional context callbacks supplied by the application.
type ContextServices struct {
	ContextSnapshot  func() ContextSnapshot
	WorkspaceChanges func() WorkspaceChanges
	MemoryView       func(MemoryScope) MemoryView
}

// SkillsServices contains optional skills callbacks supplied by the application.
type SkillsServices struct {
	SkillManagementView    func() SkillManagementView
	SelectSkill            func(string) error
	ClearSelectedSkill     func()
	RefreshSkillManagement func() SkillManagementView
	SetPluginEnabled       func(string, bool) (SkillManagementView, error)
}

// ConfigServices contains optional config callbacks supplied by the application.
type ConfigServices struct {
	ConfigView          func() ConfigView
	ResolveModelProfile func(ModelSpec) ModelProfile
	ApplyConfig         func(ConfigView) (ConfigView, error)
}

// SessionsServices contains optional sessions callbacks supplied by the application.
type SessionsServices struct {
	CurrentSessionID func() string
	ListSessions     func() []SessionMeta
	SessionMessages  func() []DisplayMessage
	ResumeSession    func(string) error
	NewSession       func() (SessionMeta, error)
	DeleteSession    func(string) error
}

// ToolsServices contains optional tools callbacks supplied by the application.
type ToolsServices struct {
	Checkpoints func() ([]CheckpointInfo, error)
	ToolNames   func() []string
	Rewind      func(string) (string, error)
}

// MCPServices contains optional mcp callbacks supplied by the application.
type MCPServices struct {
	MCPAuthorizationAction func(string, string) error
	ReplaceMCPServers      func(context.Context, string, []MCPServerSpec) error
}

// LifecycleServices contains optional lifecycle callbacks supplied by the application.
type LifecycleServices struct {
	Metrics func() MetricsSnapshot
	Close   func() error
}

// Grouped converts the legacy flat callback table at the composition boundary.
func (s Services) Grouped() ServiceGroups {
	return ServiceGroups{
		Commands: CommandsServices{
			ExecuteCommand:      s.ExecuteCommand,
			ExecuteLocalCommand: s.ExecuteLocalCommand,
			CommandMenu:         s.CommandMenu,
			Steer:               s.Steer,
		},
		Models: ModelsServices{
			CurrentModel:            s.CurrentModel,
			SwitchModel:             s.SwitchModel,
			SwitchSessionModel:      s.SwitchSessionModel,
			ModelOptions:            s.ModelOptions,
			SetReasoningEffort:      s.SetReasoningEffort,
			SetSessionReasoning:     s.SetSessionReasoning,
			ImageAttachmentsEnabled: s.ImageAttachmentsEnabled,
			VisionEnabled:           s.VisionEnabled,
		},
		Permissions: PermissionsServices{
			PermissionMode: s.PermissionMode,
			SetFullAccess:  s.SetFullAccess,
		},
		Context: ContextServices{
			ContextSnapshot:  s.ContextSnapshot,
			WorkspaceChanges: s.WorkspaceChanges,
			MemoryView:       s.MemoryView,
		},
		Skills: SkillsServices{
			SkillManagementView:    s.SkillManagementView,
			SelectSkill:            s.SelectSkill,
			ClearSelectedSkill:     s.ClearSelectedSkill,
			RefreshSkillManagement: s.RefreshSkillManagement,
			SetPluginEnabled:       s.SetPluginEnabled,
		},
		Config: ConfigServices{
			ConfigView:          s.ConfigView,
			ResolveModelProfile: s.ResolveModelProfile,
			ApplyConfig:         s.ApplyConfig,
		},
		Sessions: SessionsServices{
			CurrentSessionID: s.CurrentSessionID,
			ListSessions:     s.ListSessions,
			SessionMessages:  s.SessionMessages,
			ResumeSession:    s.ResumeSession,
			NewSession:       s.NewSession,
			DeleteSession:    s.DeleteSession,
		},
		Tools: ToolsServices{
			Checkpoints: s.Checkpoints,
			ToolNames:   s.ToolNames,
			Rewind:      s.Rewind,
		},
		MCP: MCPServices{
			MCPAuthorizationAction: s.MCPAuthorizationAction,
			ReplaceMCPServers:      s.ReplaceMCPServers,
		},
		Lifecycle: LifecycleServices{
			Metrics: s.Metrics,
			Close:   s.Close,
		},
	}
}
