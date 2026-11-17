package a2aapi

import (
	"encoding/json"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"

	controlruntime "nekocode/runtime"
)

func messageText(message *a2a.Message) (string, error) {
	if message == nil || message.Role != a2a.MessageRoleUser {
		return "", a2a.NewError(a2a.ErrInvalidParams, "a user message is required")
	}
	var texts []string
	totalBytes := 0
	for _, part := range message.Parts {
		if part == nil {
			return "", a2a.NewError(a2a.ErrInvalidParams, "message contains an empty part")
		}
		if text := strings.TrimSpace(part.Text()); text != "" {
			totalBytes += len(text)
			if totalBytes > maxMessagePartBytes {
				return "", a2a.NewError(a2a.ErrInvalidParams, "message text exceeds size limit")
			}
			texts = append(texts, text)
			continue
		}
		if data := part.Data(); data != nil {
			encoded, err := json.Marshal(data)
			if err != nil || len(encoded) > maxMessagePartBytes {
				return "", a2a.NewError(a2a.ErrInvalidParams, "message data exceeds size limit")
			}
			continue
		}
		return "", a2a.NewError(a2a.ErrUnsupportedContentType, "NekoCode A2A currently accepts text/plain and structured interaction replies")
	}
	text := strings.TrimSpace(strings.Join(texts, "\n"))
	if text == "" && !hasDataPart(message) {
		return "", a2a.NewError(a2a.ErrInvalidParams, "message contains no text")
	}
	return text, nil
}

func validateProtocolIDs(execCtx *a2asrv.ExecutorContext) error {
	if execCtx == nil || execCtx.Message == nil {
		return a2a.NewError(a2a.ErrInvalidParams, "a message is required")
	}
	return validateMessageIDs(execCtx.TaskID, execCtx.ContextID, execCtx.Message.ID)
}

func validateIncomingMessage(message *a2a.Message) error {
	if message == nil {
		return a2a.NewError(a2a.ErrInvalidParams, "a message is required")
	}
	if err := validateMessageIDs(message.TaskID, message.ContextID, message.ID); err != nil {
		return err
	}
	_, err := messageText(message)
	return err
}

func validateMessageIDs(taskID a2a.TaskID, contextID, messageID string) error {
	if len(taskID) > maxProtocolIDBytes || len(contextID) > maxProtocolIDBytes || len(messageID) > maxProtocolIDBytes {
		return a2a.NewError(a2a.ErrInvalidParams, "task, context, or message ID exceeds size limit")
	}
	return nil
}

func hasDataPart(message *a2a.Message) bool {
	for _, part := range message.Parts {
		if part.Data() != nil {
			return true
		}
	}
	return false
}

func approvalDecision(message *a2a.Message, text string) (bool, bool) {
	var structured struct {
		Allowed *bool `json:"allowed"`
	}
	if decodeDataPart(message, &structured) && structured.Allowed != nil {
		return *structured.Allowed, true
	}
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "allow", "approve", "yes", "y", "允许", "批准", "是":
		return true, true
	case "deny", "reject", "no", "n", "拒绝", "否":
		return false, true
	default:
		return false, false
	}
}

func questionReply(message *a2a.Message, text string, count int) controlruntime.QuestionReply {
	var structured controlruntime.QuestionReply
	if decodeDataPart(message, &structured) && (structured.Rejected || len(structured.Answers) > 0) {
		return structured
	}
	answers := make([][]string, count)
	if count > 0 {
		answers[0] = []string{text}
	}
	return controlruntime.QuestionReply{Answers: answers}
}

func decodeDataPart(message *a2a.Message, target any) bool {
	for _, part := range message.Parts {
		data := part.Data()
		if data == nil {
			continue
		}
		encoded, err := json.Marshal(data)
		if err == nil && json.Unmarshal(encoded, target) == nil {
			return true
		}
	}
	return false
}

func statusMessage(execCtx *a2asrv.ExecutorContext, text string, data map[string]any) *a2a.Message {
	parts := []*a2a.Part{a2a.NewTextPart(text)}
	if data != nil {
		// SDK storage deep-copies parts through gob. Keep structured wire data
		// JSON-native rather than embedding runtime-specific Go slice types.
		encoded, err := json.Marshal(data)
		var normalized map[string]any
		if err == nil && json.Unmarshal(encoded, &normalized) == nil {
			parts = append(parts, a2a.NewDataPart(normalized))
		}
	}
	return a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx, parts...)
}

func senderName(execCtx *a2asrv.ExecutorContext) string {
	if execCtx.User != nil && execCtx.User.Name != "" {
		return execCtx.User.Name
	}
	return "A2A client"
}

func deltaText(payload any) string {
	if delta, ok := payload.(controlruntime.DeltaPayload); ok {
		return delta.Delta
	}
	return ""
}

func messageContent(payload any) string {
	if message, ok := payload.(controlruntime.MessagePayload); ok {
		return message.Content
	}
	return ""
}

func runError(payload any) string {
	if result, ok := payload.(controlruntime.RunResult); ok && result.Error != "" {
		return result.Error
	}
	return "NekoCode run failed"
}

func formatQuestions(view controlruntime.QuestionView) string {
	var lines []string
	for _, question := range view.Questions {
		lines = append(lines, question.Question)
	}
	if len(lines) == 0 {
		return "Additional input is required."
	}
	return strings.Join(lines, "\n")
}
