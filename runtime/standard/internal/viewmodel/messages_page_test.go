package viewmodel

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"nekocode/bot/provider/types"
	controlruntime "nekocode/runtime"
)

func TestMessagePagesPreserveGroupingVisibilityAndImages(t *testing.T) {
	imagePath := filepath.Join(t.TempDir(), "result.png")
	f, err := os.Create(imagePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	history := []types.Message{
		{Role: "user", Source: types.MessageSourceHint, Content: "hidden"},
		{Role: "assistant", ReasoningContent: "hidden reasoning"},
		{Role: "user", Content: "question"},
		{Role: "assistant", ToolCalls: []types.ToolCall{{ID: "read", Function: types.FunctionCall{Name: "read"}}}},
		{Role: "tool", ToolCallID: "read", Content: "hidden tool"},
		{Role: "assistant", ToolCalls: []types.ToolCall{{ID: "shell", Function: types.FunctionCall{Name: "shell", Arguments: `{"command":"test"}`}}}},
		{Role: "tool", ToolCallID: "shell", Content: "\x1b[32mok\x1b[0m\r\n", IsError: true},
		{Role: "assistant", Content: "answer"},
		{Role: "system", Content: "notice"},
		{Role: "user", Content: "image please"},
		{Role: "assistant", ToolCalls: []types.ToolCall{{ID: "image", Function: types.FunctionCall{Name: "image_gen"}}}},
		{Role: "tool", ToolCallID: "image", Content: imagePath},
		{Role: "user", Source: types.MessageSourceRuntimeContext, Content: "hidden boundary"},
		{Role: "assistant", ToolCalls: []types.ToolCall{{ID: "missing", Function: types.FunctionCall{Name: "image_gen"}}}},
		{Role: "tool", ToolCallID: "missing", Content: filepath.Join(t.TempDir(), "missing.png")},
	}
	want := DisplayMessages(history)
	if len(want) != 5 {
		t.Fatalf("visible groups = %d, want 5", len(want))
	}
	for size := 1; size <= 6; size++ {
		var all []controlruntime.DisplayMessage
		for offset := 0; offset < len(want); {
			page, err := DisplayMessagesPage(history, offset, size)
			if err != nil {
				t.Fatal(err)
			}
			if page.Total != len(want) || page.NextOffset != offset+len(page.Messages) || page.HasMore != (page.NextOffset < len(want)) {
				t.Fatalf("inconsistent page: %+v", page)
			}
			all = append(all, page.Messages...)
			offset = page.NextOffset
		}
		if !reflect.DeepEqual(all, want) {
			t.Fatalf("page size %d differs from full history", size)
		}
	}
	end, err := DisplayMessagesPage(history, len(want), 1)
	if err != nil || end.HasMore || len(end.Messages) != 0 || end.NextOffset != len(want) {
		t.Fatalf("end page = %+v, %v", end, err)
	}
	for _, args := range [][2]int{{-1, 1}, {0, 0}, {0, 1001}, {len(want) + 1, 1}} {
		if _, err := DisplayMessagesPage(history, args[0], args[1]); err == nil {
			t.Fatalf("accepted invalid page %v", args)
		}
	}
}

func BenchmarkSessionHistoryPage(b *testing.B) {
	var history []types.Message
	output := strings.Repeat("\x1b[32mtool output contents\x1b[0m\n", 512)
	for j := 0; j < 1000; j++ {
		id := fmt.Sprintf("c%d", j)
		history = append(history, types.Message{Role: "user", Content: "question"}, types.Message{Role: "assistant", ToolCalls: []types.ToolCall{{ID: id, Function: types.FunctionCall{Name: "shell", Arguments: "{}"}}}}, types.Message{Role: "tool", ToolCallID: id, Content: output}, types.Message{Role: "assistant", Content: "answer"})
	}
	for _, mode := range []string{"full_then_page", "page_callback"} {
		b.Run(mode, func(b *testing.B) {
			b.ReportAllocs()
			for j := 0; j < b.N; j++ {
				if mode == "full_then_page" {
					messages := DisplayMessages(history)
					if len(messages[1900:2000]) != 100 {
						b.Fatal("invalid page")
					}
				} else {
					page, err := DisplayMessagesPage(history, 1900, 100)
					if err != nil || len(page.Messages) != 100 || page.Total != 2000 {
						b.Fatalf("invalid page: %+v, %v", page, err)
					}
				}
			}
		})
	}
}
