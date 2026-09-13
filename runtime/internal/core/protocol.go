// Package core defines the stable runtime protocol shared by the public
// runtime package and internal services.
package core

import (
	"context"
	"time"

	"nekocode/protocol"
)

type RunID string

const (
	ProtocolVersion = "2.0"
)

type SourceRef struct {
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`
}

type SenderRef struct {
	ID       string `json:"id,omitempty"`
	Username string `json:"username,omitempty"`
	Display  string `json:"display,omitempty"`
}

type Input struct {
	Source SourceRef `json:"source"`
	Sender SenderRef `json:"sender"`
	Text   string    `json:"text"`
}

type RunStatus string

const (
	RunIdle            RunStatus = "idle"
	RunRunning         RunStatus = "running"
	RunWaitingApproval RunStatus = "waiting_approval"
	RunWaitingQuestion RunStatus = "waiting_question"
	RunDone            RunStatus = "done"
	RunFailed          RunStatus = "failed"
	RunCancelled       RunStatus = "aborted"
)

type EventType string

const (
	EventInputAccepted     EventType = "input_accepted"
	EventSystemMessage     EventType = "system_message"
	EventAssistantDelta    EventType = "assistant_delta"
	EventReasoningDelta    EventType = "reasoning_delta"
	EventPhaseChanged      EventType = "phase_changed"
	EventCompaction        EventType = "compaction"
	EventToolStarted       EventType = "tool_started"
	EventToolBlocked       EventType = "tool_blocked"
	EventToolPreview       EventType = "tool_preview"
	EventToolCompleted     EventType = "tool_completed"
	EventSubAgentStarted   EventType = "subagent_started"
	EventSubAgentEnded     EventType = "subagent_ended"
	EventTodosUpdated      EventType = "todos_updated"
	EventApprovalRequested EventType = "approval_requested"
	EventApprovalResolved  EventType = "approval_resolved"
	EventQuestionRequested EventType = "question_requested"
	EventQuestionResolved  EventType = "question_resolved"
	EventRunStarted        EventType = "run_started"
	EventRunDone           EventType = "run_done"
	EventRunFailed         EventType = "run_failed"
	EventRunCancelled      EventType = "run_aborted"
	EventSessionChanged    EventType = "session_changed"
	EventConnectorStatus   EventType = "connector_status"
	EventMetricsUpdated    EventType = "metrics_updated"
)

type Event struct {
	Version  string    `json:"version,omitempty"`
	ID       string    `json:"id"`
	Sequence uint64    `json:"sequence,omitempty"`
	RunID    RunID     `json:"run_id,omitempty"`
	Type     EventType `json:"type"`
	Source   SourceRef `json:"source"`
	Time     time.Time `json:"time"`
	Payload  any       `json:"payload,omitempty"`
}

type EventFilter struct {
	RunID   RunID
	After   uint64
	Types   []EventType
	Sources []string
}

type MessagePayload struct {
	Role    string    `json:"role"`
	Content string    `json:"content"`
	Source  SourceRef `json:"source"`
	Sender  SenderRef `json:"sender"`
}

type DeltaPayload struct {
	Delta string `json:"delta"`
	Done  bool   `json:"done,omitempty"`
}

type PhasePayload struct {
	Phase string `json:"phase"`
}

type ToolPayload struct {
	ToolName      string `json:"tool_name"`
	CallID        string `json:"call_id,omitempty"`
	Args          string `json:"args,omitempty"`
	Output        string `json:"output,omitempty"`
	Preview       string `json:"preview,omitempty"`
	IsError       bool   `json:"is_error,omitempty"`
	SubAgentID    string `json:"subagent_id,omitempty"`
	SubAgentColor int    `json:"subagent_color,omitempty"`
}

type SubAgentPayload struct {
	ID      string   `json:"id"`
	Type    string   `json:"type,omitempty"` // compatibility alias for Profile
	Profile string   `json:"profile,omitempty"`
	Skills  []string `json:"skills,omitempty"`
	Color   int      `json:"color,omitempty"`
}

type SessionPayload struct {
	ID string `json:"id,omitempty"`
}

type RunResult struct {
	Output string `json:"output"`
	Error  string `json:"error,omitempty"`
}

// ConnectorRuntime is the narrow host contract supplied to IM connectors.
type ConnectorRuntime interface {
	StartRun(ctx context.Context, input Input) (RunID, error)
	CancelRun(ctx context.Context, runID RunID) error
	DecideApproval(ctx context.Context, approvalID string, decision ApprovalDecision) error
	AnswerQuestion(ctx context.Context, questionID string, reply protocol.QuestionReply) error
	CommandMenu(ctx context.Context, input string) (protocol.CommandMenu, bool)
	ExecuteLocalCommand(ctx context.Context, input string) (string, protocol.LocalCommandResult)
	Events(ctx context.Context, filter EventFilter) (<-chan Event, error)
	ReportConnectorStatus(payload ConnectorStatusPayload)
}
