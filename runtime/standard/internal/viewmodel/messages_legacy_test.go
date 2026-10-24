package viewmodel

import (
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"nekocode/bot/provider/types"
	controlruntime "nekocode/runtime"
)

// Compare the optimized projection with the pre-optimization behavior across
// mixed roles, hidden messages and tool results, not just hand-picked pages.
func TestRandomPagesMatchLegacyProjection(t *testing.T) {
	rng := rand.New(rand.NewSource(20261024))
	roles := []string{"user", "assistant", "tool", "system", "unknown"}
	contents := []string{"", " ", "answer", "\nnotice\n", "\x1b[32mok\x1b[0m\r\n", "---"}
	names := []string{"read", "shell", "write", "image_gen", "question"}
	for trial := 0; trial < 300; trial++ {
		var history []types.Message
		for j := 0; j < rng.Intn(40)+20; j++ {
			m := types.Message{Role: roles[rng.Intn(len(roles))], Content: contents[rng.Intn(len(contents))]}
			if rng.Intn(5) == 0 {
				m.Source = types.MessageSourceHint
			}
			if m.Role == "assistant" && rng.Intn(2) == 0 {
				m.ToolCalls = []types.ToolCall{{ID: fmt.Sprint(rng.Intn(7)), Function: types.FunctionCall{Name: names[rng.Intn(len(names))], Arguments: "{}"}}}
			}
			if m.Role == "tool" {
				m.ToolCallID = fmt.Sprint(rng.Intn(7))
				m.IsError = rng.Intn(2) == 0
			}
			history = append(history, m)
		}
		want := legacyDisplayMessages(history)
		if got := DisplayMessages(history); !reflect.DeepEqual(got, want) {
			t.Fatalf("trial%d full differs\nhistory=%+v\ngot=%+v\nwant=%+v", trial, history, got, want)
		}
		for offset := 0; offset <= len(want); offset++ {
			limit := rng.Intn(5) + 1
			page, err := DisplayMessagesPage(history, offset, limit)
			if err != nil {
				t.Fatal(err)
			}
			end := min(offset+limit, len(want))
			expected := want[offset:end]
			if page.Total != len(want) || page.NextOffset != end || page.HasMore != (end < len(want)) || len(page.Messages) != len(expected) || (len(expected) > 0 && !reflect.DeepEqual(page.Messages, expected)) {
				t.Fatalf("trial%d offset%d page=%+v want=%+v", trial, offset, page, expected)
			}
		}
	}
}

// Frozen reference from the implementation before pagination optimization.
func legacyDisplayMessages(messages []types.Message) []controlruntime.DisplayMessage {
	toolNames, toolArgs := toolMetaByID(messages)
	var out []controlruntime.DisplayMessage
	i := 0
	for i < len(messages) {
		m := messages[i]
		switch m.Role {
		case "user":
			if !isInternalMessage(m) {
				out = append(out, controlruntime.DisplayMessage{Role: "user", Content: controlruntime.VisibleInputText(m.Content)})
			}
			i++
		case "assistant":
			msg, next := legacyDisplayAssistantTurn(messages, i, toolNames, toolArgs)
			if msg.Content != "" || len(msg.Blocks) > 0 || len(msg.Images) > 0 {
				out = append(out, msg)
			}
			i = next
		case "system":
			if !isInternalMessage(m) {
				out = append(out, controlruntime.DisplayMessage{Role: "system", Content: m.Content})
			}
			i++
		default:
			i++
		}
	}
	return out
}

func legacyDisplayAssistantTurn(msgs []types.Message, idx int, toolNames, toolArgs map[string]string) (controlruntime.DisplayMessage, int) {
	var contentParts []string
	var blocks []controlruntime.DisplayBlock
	var images []controlruntime.ImageRef

	next := idx
	for next < len(msgs) && msgs[next].Role == "assistant" {
		m := msgs[next]
		next++

		if len(m.ToolCalls) == 0 {
			if content := strings.TrimSpace(m.Content); content != "" && !isInternalMessage(m) {
				contentParts = append(contentParts, content)
			}
			continue
		}

		for next < len(msgs) && msgs[next].Role == "tool" {
			name := toolNames[msgs[next].ToolCallID]
			blocks, images = appendDisplayToolResult(blocks, images, name, toolArgs[msgs[next].ToolCallID], msgs[next])
			next++
		}
	}

	return controlruntime.DisplayMessage{
		Role:    "assistant",
		Content: strings.Join(contentParts, "\n\n"),
		Blocks:  blocks,
		Images:  images,
	}, next
}
