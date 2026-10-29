// Package headless implements NekoCode's versioned, bidirectional NDJSON
// protocol for NekoCode hosts. See docs/developers/STREAM_JSON.md for the supported contract.
package headless

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	OutputFormat           string
	OutputSchema           string
	once                   bool
	schema                 *onceSchema
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

type userInput struct {
	id     string
	text   string
	images []rt.ImageAttachment
}

// blockImageMarkerN numbers image markers within one user message starting
// at 1; the marker must appear in the message text for the runtime's
// attachment envelope to accept the image.
func (f frame) user() (userInput, error) {
	if f.Message.Role != "user" || f.ParentToolUseID != nil || (f.ShouldQuery != nil && !*f.ShouldQuery) || (f.Priority != "" && f.Priority != "next") {
		return userInput{}, errors.New("only root user messages with shouldQuery=true and FIFO priority are supported")
	}
	var text string
	var images []rt.ImageAttachment
	if json.Unmarshal(f.Message.Content, &text) == nil {
		// Plain string content: no images possible.
	} else {
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
			Path string `json:"path"`
		}
		if json.Unmarshal(f.Message.Content, &blocks) != nil {
			return userInput{}, errors.New("content must be text or content blocks")
		}
		parts := make([]string, 0, len(blocks))
		imageCount := 0
		for _, block := range blocks {
			switch block.Type {
			case "text":
				parts = append(parts, block.Text)
			case "image":
				if strings.TrimSpace(block.Path) == "" {
					return userInput{}, errors.New("image block requires a path")
				}
				imageCount++
				marker := fmt.Sprintf("[Image #%d]", imageCount)
				parts = append(parts, marker)
				images = append(images, rt.ImageAttachment{Label: marker, Path: block.Path})
			default:
				return userInput{}, fmt.Errorf("unsupported content block type %q", block.Type)
			}
		}
		text = strings.Join(parts, "\n\n")
	}
	if strings.TrimSpace(text) == "" {
		return userInput{}, errors.New("empty user message")
	}
	return userInput{id: f.UUID, text: text, images: images}, nil
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
	// ContextSnapshot reports the active context window's composition. It is
	// read-only, so hosts may poll it while the turn is running.
	ContextSnapshot() rt.ContextSnapshot
	// Observation projections shared with the HTTP /observe/* surface. They
	// are read-only and safe during an active run.
	ObservedOverview() rt.ObserveOverview
	ObservedRuns(rt.ObservedRunFilter) []rt.ObservedRunSummary
	ObservedRun(rt.RunID) (rt.ObservedRunDetail, bool)
}

var _ Management = (*rt.Runtime)(nil)
