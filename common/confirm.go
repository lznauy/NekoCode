package common

// ConfirmRequest is sent to the TUI when a tool requires user approval.
type ConfirmRequest struct {
	ToolName string
	Args     map[string]any
	Level    DangerLevel
	Response chan bool
}

// ConfirmFunc asks the user to approve a tool call.
type ConfirmFunc func(req ConfirmRequest) bool

// PhaseFunc is called when the agent's phase changes.
type PhaseFunc func(phase string)

// Phase constants — emitted by agent, displayed by TUI status line.
const (
	PhaseReady     = "Ready"
	PhaseWaiting   = "Waiting"
	PhaseThinking  = "Thinking"
	PhaseReasoning = "Reasoning"
	PhaseRunning   = "Running"
)
