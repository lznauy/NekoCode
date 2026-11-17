package a2aapi

import (
	"fmt"

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
	data := map[string]any{"type": string(pending.kind), "interactionId": pending.id}
	if task := execCtx.StoredTask; task != nil && task.Status.Message != nil {
		for _, part := range task.Status.Message.Parts {
			if original, ok := part.Data().(map[string]any); ok && original["interactionId"] == pending.id {
				data = original
				break
			}
		}
	}
	return statusMessage(execCtx, reason, data)
}
