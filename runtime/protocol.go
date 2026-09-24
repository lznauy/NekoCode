package runtime

import (
	"fmt"

	"nekocode/protocol"
	"nekocode/runtime/internal/core"
)

const ProtocolVersion = core.ProtocolVersion

type RunID = core.RunID

type SourceRef = core.SourceRef
type SenderRef = core.SenderRef
type Input = core.Input

type RunStatus = core.RunStatus

const (
	RunIdle            = core.RunIdle
	RunRunning         = core.RunRunning
	RunWaitingApproval = core.RunWaitingApproval
	RunWaitingQuestion = core.RunWaitingQuestion
	RunDone            = core.RunDone
	RunFailed          = core.RunFailed
	RunCancelled       = core.RunCancelled
)

type EventType = core.EventType

const (
	EventInputAccepted     = core.EventInputAccepted
	EventSystemMessage     = core.EventSystemMessage
	EventAssistantDelta    = core.EventAssistantDelta
	EventAssistantMessage  = core.EventAssistantMessage
	EventReasoningDelta    = core.EventReasoningDelta
	EventPhaseChanged      = core.EventPhaseChanged
	EventCompaction        = core.EventCompaction
	EventToolStarted       = core.EventToolStarted
	EventToolBlocked       = core.EventToolBlocked
	EventToolPreview       = core.EventToolPreview
	EventToolCompleted     = core.EventToolCompleted
	EventSubAgentStarted   = core.EventSubAgentStarted
	EventSubAgentEnded     = core.EventSubAgentEnded
	EventTodosUpdated      = core.EventTodosUpdated
	EventApprovalRequested = core.EventApprovalRequested
	EventApprovalResolved  = core.EventApprovalResolved
	EventQuestionRequested = core.EventQuestionRequested
	EventQuestionResolved  = core.EventQuestionResolved
	EventRunStarted        = core.EventRunStarted
	EventRunSummary        = core.EventRunSummary
	EventSubAgentOutput    = core.EventSubAgentOutput
	EventRunDone           = core.EventRunDone
	EventRunFailed         = core.EventRunFailed
	EventRunCancelled      = core.EventRunCancelled
	EventSessionChanged    = core.EventSessionChanged
	EventConnectorStatus   = core.EventConnectorStatus
	EventMetricsUpdated    = core.EventMetricsUpdated
)

type Event = core.Event
type EventFilter = core.EventFilter
type MessagePayload = core.MessagePayload
type DeltaPayload = core.DeltaPayload
type PhasePayload = core.PhasePayload
type CompactionPayload = protocol.CompactionEvent
type TodoItem = protocol.TodoItem

// LocalCommandResult and its values classify ExecuteLocalCommand outcomes.
type LocalCommandResult = protocol.LocalCommandResult

const (
	LocalCommandNotCommand   = protocol.LocalCommandNotCommand
	LocalCommandExecuted     = protocol.LocalCommandExecuted
	LocalCommandRequiresIdle = protocol.LocalCommandRequiresIdle
)

// MetricsSnapshot is independent from run lifecycle status.
type MetricsSnapshot = protocol.Metrics
type WorkspaceChanges = protocol.WorkspaceChanges
type CheckpointInfo = protocol.CheckpointInfo
type ToolPayload = core.ToolPayload
type SubAgentPayload = core.SubAgentPayload
type SessionPayload = core.SessionPayload
type RunResult = core.RunResult

// ModelSelection identifies the active provider and model.
type ModelSelection struct {
	Provider        string `json:"provider"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
}

// ModelOption describes one selectable model configuration for session
// config surfaces. ReasoningEfforts lists the effort values the model
// accepts; it is empty for models without controllable reasoning.
type ModelOption struct {
	Name             string   `json:"name"`
	Model            string   `json:"model"`
	ReasoningEffort  string   `json:"reasoning_effort,omitempty"`
	ReasoningEfforts []string `json:"reasoning_efforts,omitempty"`
}

// ContextSegment describes one visible part of the active context window.
type ContextSegment struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Tokens int    `json:"tokens"`
	Tone   string `json:"tone"`
}

// ContextSnapshot is the structured context status exposed by a Runner.
type ContextSnapshot struct {
	Budget              int              `json:"budget"`
	Used                int              `json:"used"`
	Free                int              `json:"free"`
	PercentUsed         float64          `json:"percentUsed"`
	SystemPrompt        int              `json:"systemPrompt"`
	ToolDefTokens       int              `json:"toolDefTokens"`
	TodoText            int              `json:"todoText"`
	SkillList           int              `json:"skillList"`
	MessageTokens       int              `json:"messageTokens"`
	ToolDefCount        int              `json:"toolDefCount"`
	MessageCount        int              `json:"messageCount"`
	UserMessages        int              `json:"userMessages"`
	AssistantMsgs       int              `json:"assistantMsgs"`
	ToolResults         int              `json:"toolResults"`
	Archived            int              `json:"archived"`
	CompactCount        int              `json:"compactCount"`
	CompactionThreshold int              `json:"compactionThreshold"`
	TrimCount           int              `json:"trimCount"`
	CacheHitTokens      int              `json:"cacheHitTokens"`
	CacheMissTokens     int              `json:"cacheMissTokens"`
	CacheHitRatio       float64          `json:"cacheHitRatio"`
	SubCount            int              `json:"subCount"`
	SubTokens           int              `json:"subTokens"`
	SubCacheHit         int              `json:"subCacheHit"`
	SubCacheMiss        int              `json:"subCacheMiss"`
	Segments            []ContextSegment `json:"segments"`
}

type MemoryScope string

const MemoryScopeProject MemoryScope = "project"

type MemorySection struct {
	Key     string `json:"key"`
	Title   string `json:"title"`
	Content string `json:"content"`
	Empty   bool   `json:"empty"`
}

type MemoryView struct {
	Scope    MemoryScope     `json:"scope"`
	Path     string          `json:"path,omitempty"`
	Content  string          `json:"content"`
	Sections []MemorySection `json:"sections,omitempty"`
	Empty    bool            `json:"empty"`
}

// SessionMeta is a lightweight descriptor for a persisted session.
type SessionMeta struct {
	ID        string `json:"id"`
	CWD       string `json:"cwd"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
	MsgCount  int    `json:"msgCount"`
}

// DisplayMessage is a persistent conversation projection for interaction
// surfaces.
type DisplayMessage struct {
	Role      string         `json:"role"`
	Content   string         `json:"content"`
	Reasoning string         `json:"reasoning,omitempty"`
	Blocks    []DisplayBlock `json:"blocks,omitempty"`
	Images    []ImageRef     `json:"images,omitempty"`
}

type DisplayBlock struct {
	ToolName string `json:"toolName"`
	Args     string `json:"args,omitempty"`
	Content  string `json:"content"`
	IsError  bool   `json:"isError,omitempty"`
}

type ImageRef struct {
	Path   string `json:"path"`
	URL    string `json:"url,omitempty"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

// ConfigView is the configuration contract exposed to interaction surfaces.
type ConfigView struct {
	Path               string                     `json:"path"`
	Exists             bool                       `json:"exists"`
	Active             string                     `json:"active"`
	AutoCompactPercent int                        `json:"auto_compact_percent"`
	FlashModel         string                     `json:"flash_model,omitempty"`
	Models             []ModelConfig              `json:"models"`
	ImageGenModels     []ImageGenConfig           `json:"image_gen_models,omitempty"`
	MCPServers         map[string]MCPServerConfig `json:"mcp_servers,omitempty"`
	Permissions        *PermissionsConfig         `json:"permissions,omitempty"`
	Workspaces         []WorkspaceConfig          `json:"workspaces,omitempty"`
	Jev                *JevConfig                 `json:"jev,omitempty"`
}

type JevConfig struct {
	APIKey        string   `json:"api_key,omitempty"`
	Model         string   `json:"model,omitempty"`
	BaseURL       string   `json:"base_url,omitempty"`
	KeepThreshold *float64 `json:"keep_threshold,omitempty"`
	Enabled       *bool    `json:"enabled,omitempty"`
}

type ModelConfig struct {
	Name            string       `json:"name"`
	Provider        string       `json:"provider"`
	APIKey          string       `json:"api_key"`
	Model           string       `json:"model"`
	BaseURL         string       `json:"base_url,omitempty"`
	Protocol        string       `json:"protocol,omitempty"`
	ReasoningEffort string       `json:"reasoning_effort,omitempty"`
	ContextWindow   int          `json:"context_window,omitempty"`
	Profile         ModelProfile `json:"profile"`
}

// ModelProfile contains derived model metadata. It is read-only projection
// state: only ModelConfig's explicit fields are persisted.
type ModelProfile struct {
	ContextWindow       int      `json:"context_window"`
	ContextWindowSource string   `json:"context_window_source"`
	ReasoningEfforts    []string `json:"reasoning_efforts,omitempty"`
}

// ModelSpec contains only the fields needed to resolve derived model metadata.
type ModelSpec struct {
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	Protocol      string `json:"protocol,omitempty"`
	ContextWindow int    `json:"context_window,omitempty"`
}

type ImageGenConfig struct {
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	APIKey    string `json:"api_key"`
	SecretKey string `json:"secret_key"`
	BaseURL   string `json:"base_url,omitempty"`
	Model     string `json:"model,omitempty"`
}

type MCPServerConfig struct {
	URL                    string            `json:"url,omitempty"`
	Headers                map[string]string `json:"headers,omitempty"`
	OAuthClientID          string            `json:"oauth_client_id,omitempty"`
	OAuthClientSecret      string            `json:"oauth_client_secret,omitempty"`
	OAuthClientMetadataURL string            `json:"oauth_client_metadata_url,omitempty"`
	OAuthCallbackPort      int               `json:"oauth_callback_port,omitempty"`

	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Enabled bool              `json:"enabled"`
}

// MCPServerSpec names one transport-supplied MCP server configuration.
type MCPServerSpec struct {
	Name   string          `json:"name"`
	Config MCPServerConfig `json:"config"`
}

type PermissionsConfig struct {
	Allow   []string                 `json:"allow,omitempty"`
	Ask     []string                 `json:"ask,omitempty"`
	Deny    []string                 `json:"deny,omitempty"`
	Sandbox map[string]SandboxConfig `json:"sandbox,omitempty"`
}

type SandboxConfig struct {
	SandboxMode   string   `json:"sandbox_mode,omitempty"`
	Network       bool     `json:"network,omitempty"`
	WritableRoots []string `json:"writable_roots,omitempty"`
}

type WorkspaceConfig struct {
	Path   string `json:"path"`
	Access string `json:"access,omitempty"`
}

// SkillManagementView is the extension management projection exposed by a
// Runner.
type SkillManagementView struct {
	Skills  []SkillView     `json:"skills"`
	Plugins []PluginView    `json:"plugins"`
	MCP     []MCPServerView `json:"mcp"`
}

type SkillView struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Dir         string   `json:"dir,omitempty"`
	Files       []string `json:"files,omitempty"`
	Loaded      bool     `json:"loaded"`
	Source      string   `json:"source"`
	SourceKind  string   `json:"sourceKind"`
	Plugin      string   `json:"plugin,omitempty"`
}

type PluginView struct {
	Name         string   `json:"name"`
	Version      string   `json:"version,omitempty"`
	Description  string   `json:"description,omitempty"`
	Source       string   `json:"source,omitempty"`
	Dir          string   `json:"dir,omitempty"`
	Enabled      bool     `json:"enabled"`
	Skills       []string `json:"skills,omitempty"`
	SkillNames   []string `json:"skillNames,omitempty"`
	Agents       []string `json:"agents,omitempty"`
	Commands     []string `json:"commands,omitempty"`
	MCPServers   []string `json:"mcpServers,omitempty"`
	HasHooks     bool     `json:"hasHooks,omitempty"`
	ActiveAgents []string `json:"activeAgents,omitempty"`
	AgentError   string   `json:"agentError,omitempty"`
}

type MCPServerView struct {
	URL           string   `json:"url,omitempty"`
	AuthURL       string   `json:"authUrl,omitempty"`
	Name          string   `json:"name"`
	Plugin        string   `json:"plugin"`
	Command       string   `json:"command"`
	Args          []string `json:"args,omitempty"`
	PluginEnabled bool     `json:"pluginEnabled"`
	Status        string   `json:"status,omitempty"`
	Error         string   `json:"error,omitempty"`
	ToolCount     int      `json:"toolCount,omitempty"`
}

type ApprovalStatus = core.ApprovalStatus

const (
	ApprovalPending  = core.ApprovalPending
	ApprovalApproved = core.ApprovalApproved
	ApprovalRejected = core.ApprovalRejected
	ApprovalExpired  = core.ApprovalExpired
)

type ConfirmKind = protocol.ConfirmKind
type ApprovalScope = protocol.ApprovalScope
type ApprovalContext = protocol.ApprovalContext

const (
	ConfirmKindPermission = protocol.ConfirmKindPermission
	ConfirmKindInstall    = protocol.ConfirmKindInstall
	ApprovalScopeOnce     = protocol.ApprovalScopeOnce
	ApprovalScopeProject  = protocol.ApprovalScopeProject
)

type ConfirmRequest = protocol.ConfirmRequest
type ConfirmReply = protocol.ConfirmReply
type ApprovalDecision = core.ApprovalDecision
type ApprovalView = core.ApprovalView
type ConnectorRuntime = core.ConnectorRuntime
type RunSnapshot = core.RunSnapshot

type ToolStatus = core.ToolStatus

const (
	ToolRunning = core.ToolRunning
	ToolDone    = core.ToolDone
	ToolBlocked = core.ToolBlocked
)

type ToolView = core.ToolView
type SubAgentView = core.SubAgentView
type ConnectorStatusPayload = core.ConnectorStatusPayload
type ConnectView = core.ConnectView
type ConnectorView = core.ConnectorView
type ConnectorDeviceView = core.ConnectorDeviceView

type QuestionStatus = core.QuestionStatus

const (
	QuestionPending  = core.QuestionPending
	QuestionAnswered = core.QuestionAnswered
	QuestionRejected = core.QuestionRejected
)

type QuestionView = core.QuestionView
type QuestionOption = protocol.QuestionOption
type QuestionItem = protocol.QuestionItem
type QuestionReply = protocol.QuestionReply
type QuestionRequest = protocol.QuestionRequest

type ErrorCode string

const (
	ErrorInvalidInput ErrorCode = "invalid_input"
	ErrorClosed       ErrorCode = "closed"
	ErrorBusy         ErrorCode = "busy"
	ErrorNotFound     ErrorCode = "not_found"
	ErrorConflict     ErrorCode = "conflict"
	ErrorUnsupported  ErrorCode = "unsupported"
)

// ProtocolError gives transports a stable code without coupling them to
// runtime's internal error strings.
type ProtocolError struct {
	Code      ErrorCode `json:"code"`
	Operation string    `json:"operation,omitempty"`
	Message   string    `json:"message"`
}

func (e *ProtocolError) Error() string {
	if e.Operation == "" {
		return "runtime: " + e.Message
	}
	return fmt.Sprintf("runtime: %s: %s", e.Operation, e.Message)
}

func protocolError(code ErrorCode, operation, message string) error {
	return &ProtocolError{Code: code, Operation: operation, Message: message}
}

type RunSummary = protocol.RunSummary
type SubAgentOutput = protocol.SubAgentOutput
