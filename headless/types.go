// Package headless implements NekoCode's versioned, bidirectional NDJSON
// protocol for NekoCode hosts. See docs/STREAM_JSON.md for the supported contract.
package headless

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	rt "nekocode/runtime"
)

const ProtocolVersion = "nekocode-headless/2"
const maxFrameSize = 8 << 20
const maxQueuedTurns = 32

type Backend interface {
	StartRun(context.Context, rt.Input) (rt.RunID, error)
	WaitRun(context.Context, rt.RunID) error
	CancelRun(context.Context, rt.RunID) error
	DecideApproval(context.Context, string, rt.ApprovalDecision) error
	AnswerQuestion(context.Context, string, rt.QuestionReply) error
	// Events must honor EventFilter.Reliable.
	Events(context.Context, rt.EventFilter) (<-chan rt.Event, error)
	CurrentSessionID() string
	NewSession() (rt.SessionMeta, error)
	ResumeSession(string) error
	CurrentModel() rt.ModelSelection
	PermissionMode() string
}

var _ Backend = (*rt.Runtime)(nil)

type Options struct {
	InputFormat            string
	Prompt                 string
	Resume                 string
	IncludePartialMessages bool
}

type frame struct {
	Type            string  `json:"type"`
	UUID            string  `json:"uuid,omitempty"`
	SessionID       string  `json:"session_id,omitempty"`
	ParentToolUseID *string `json:"parent_tool_use_id,omitempty"`
	Priority        string  `json:"priority,omitempty"`
	ShouldQuery     *bool   `json:"shouldQuery,omitempty"`
	Message         struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	RequestID string          `json:"request_id,omitempty"`
	Request   json.RawMessage `json:"request,omitempty"`
	Response  struct {
		Subtype   string          `json:"subtype"`
		RequestID string          `json:"request_id"`
		Response  json.RawMessage `json:"response"`
	} `json:"response"`
}

type userInput struct{ id, text string }

func (f frame) user() (userInput, error) {
	if f.Message.Role != "user" || f.ParentToolUseID != nil || (f.ShouldQuery != nil && !*f.ShouldQuery) || (f.Priority != "" && f.Priority != "next") {
		return userInput{}, errors.New("only root user messages with shouldQuery=true and FIFO priority are supported")
	}
	var text string
	if json.Unmarshal(f.Message.Content, &text) != nil {
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(f.Message.Content, &blocks) != nil {
			return userInput{}, errors.New("content must be text or text blocks")
		}
		parts := make([]string, 0, len(blocks))
		for _, block := range blocks {
			if block.Type != "text" {
				return userInput{}, errors.New("only text content blocks are supported")
			}
			parts = append(parts, block.Text)
		}
		text = strings.Join(parts, "\n\n")
	}
	if strings.TrimSpace(text) == "" {
		return userInput{}, errors.New("empty user message")
	}
	return userInput{id: f.UUID, text: text}, nil
}

// Management is optional for embedded backends. Runtime implements it and
// reports the actually configured services through its capability manifest.
type Management interface {
	Checkpoints() ([]rt.CheckpointInfo, error)
	Capabilities() rt.CapabilityManifest
	ModelOptions() ([]rt.ModelOption, string)
	SwitchSessionModel(string) (rt.ModelSelection, error)
	SetFullAccess(bool) error
	ListSessions() []rt.SessionMeta
	SessionMessages() []rt.DisplayMessage
	DeleteSession(string) error
	SkillManagementView() rt.SkillManagementView
	ToolNames() []string
	CommandMenu(context.Context, string) (rt.CommandMenu, bool)
	WorkspaceChanges() rt.WorkspaceChanges
	Rewind(string) (string, error)
	SteerRun(context.Context, rt.RunID, rt.Input) error
}

var _ Management = (*rt.Runtime)(nil)
