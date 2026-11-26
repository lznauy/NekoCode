package a2aapi

import (
	"fmt"
	"slices"

	rt "nekocode/runtime"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// Replies bind to one interaction, not whichever prompt happens to be pending
// when a delayed request arrives. Text replies carry the ID in metadata.
func validateInteractionReply(message *a2a.Message, expected string) error {
	found := false
	check := func(value any) error {
		id, ok := value.(string)
		if !ok || id == "" || id != expected {
			return fmt.Errorf("interactionId must match the current request %q", expected)
		}
		found = true
		return nil
	}
	if value, ok := message.Metadata["interactionId"]; ok {
		if err := check(value); err != nil {
			return err
		}
	}
	for _, part := range message.Parts {
		if data, ok := part.Data().(map[string]any); ok {
			if value, ok := data["interactionId"]; ok {
				if err := check(value); err != nil {
					return err
				}
			}
		}
	}
	if !found {
		return fmt.Errorf("reply must include interactionId %q", expected)
	}
	return nil
}

func pendingReplyMessage(execCtx *a2asrv.ExecutorContext, pending *pendingInteraction, reason string) *a2a.Message {
	// The queue owns the current prompt even when the caller's task snapshot is older.
	parts := append([]*a2a.Part{a2a.NewTextPart(reason)}, pending.message.Parts...)
	return a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx, parts...)
}

// Callers hold the binding mutex while reading or changing the pending queue.
func (b *taskBinding) firstInteraction() *pendingInteraction {
	if len(b.pending) == 0 {
		return nil
	}
	return b.pending[0]
}

// removeInteraction reports whether the prompt presented to the client changed.
func (b *taskBinding) removeInteraction(id string) bool {
	for i, p := range b.pending {
		if p.id == id {
			b.pending = slices.Delete(b.pending, i, i+1)
			return i == 0
		}
	}
	return false
}

func interactionRequest(execCtx *a2asrv.ExecutorContext, event rt.Event) *pendingInteraction {
	switch view := event.Payload.(type) {
	case rt.ApprovalView:
		data := map[string]any{"type": "approval", "interactionId": view.ID, "toolName": view.ToolName, "args": view.Args}
		text := fmt.Sprintf("Approval required for tool %q. Reply with allow or deny.", view.ToolName)
		return &pendingInteraction{kind: pendingApproval, id: view.ID, message: statusMessage(execCtx, text, data)}
	case rt.QuestionView:
		data := map[string]any{"type": "question", "interactionId": view.ID, "questions": view.Questions}
		return &pendingInteraction{kind: pendingQuestion, id: view.ID, questions: len(view.Questions), message: statusMessage(execCtx, formatQuestions(view), data)}
	}
	return nil
}
