package core

import (
	"time"

	"nekocode/protocol"
)

type RunSnapshot struct {
	ID         RunID               `json:"id"`
	Status     RunStatus           `json:"status"`
	Source     SourceRef           `json:"source"`
	Sender     SenderRef           `json:"sender"`
	Input      string              `json:"input,omitempty"`
	Phase      string              `json:"phase,omitempty"`
	StartedAt  time.Time           `json:"started_at"`
	UpdatedAt  time.Time           `json:"updated_at"`
	FinishedAt *time.Time          `json:"finished_at,omitempty"`
	Tools      []ToolView          `json:"tools,omitempty"`
	Todos      []protocol.TodoItem `json:"todos,omitempty"`
	SubAgents  []SubAgentView      `json:"subagents,omitempty"`
	Approvals  []ApprovalView      `json:"approvals,omitempty"`
	Questions  []QuestionView      `json:"questions,omitempty"`
	Output     string              `json:"output,omitempty"`
	Reasoning  string              `json:"reasoning,omitempty"`
	Error      string              `json:"error,omitempty"`
	EventCount int                 `json:"event_count"`
}

type ToolStatus string

const (
	ToolRunning ToolStatus = "running"
	ToolDone    ToolStatus = "done"
	ToolBlocked ToolStatus = "blocked"
)

type ToolView struct {
	CallID        string     `json:"call_id,omitempty"`
	Name          string     `json:"name"`
	Args          string     `json:"args,omitempty"`
	Preview       string     `json:"preview,omitempty"`
	Output        string     `json:"output,omitempty"`
	Status        ToolStatus `json:"status"`
	IsError       bool       `json:"is_error,omitempty"`
	SubAgentID    string     `json:"subagent_id,omitempty"`
	SubAgentColor int        `json:"subagent_color,omitempty"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
}

type SubAgentView struct {
	SessionID string    `json:"session_id,omitempty"`
	Status    string    `json:"status,omitempty"`
	Phase     string    `json:"phase,omitempty"`
	Prompt    string    `json:"prompt,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`

	ID         string     `json:"id"`
	Type       string     `json:"type,omitempty"` // compatibility alias for Profile
	Profile    string     `json:"profile,omitempty"`
	Skills     []string   `json:"skills,omitempty"`
	Color      int        `json:"color,omitempty"`
	Active     bool       `json:"active"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	AttemptID  string     `json:"attempt_id,omitempty"`
	AttemptSeq int        `json:"attempt_seq,omitempty"`
}

type ConnectorStatusPayload struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type ConnectView struct {
	Connectors []ConnectorView `json:"connectors"`
}

type ConnectorView struct {
	Name        string                `json:"name"`
	Registered  bool                  `json:"registered"`
	Initialized bool                  `json:"initialized"`
	Configured  bool                  `json:"configured"`
	Running     bool                  `json:"running"`
	Status      string                `json:"status"`
	Message     string                `json:"message,omitempty"`
	Devices     []ConnectorDeviceView `json:"devices,omitempty"`
	Metadata    map[string]any        `json:"metadata,omitempty"`
}

type ConnectorDeviceView struct {
	ID       string `json:"id,omitempty"`
	Username string `json:"username,omitempty"`
	Display  string `json:"display,omitempty"`
	LastSeen int64  `json:"last_seen,omitempty"`
	PairedAt int64  `json:"paired_at,omitempty"`
}
