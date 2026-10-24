package core

import (
	"fmt"
	"testing"

	"nekocode/bot/contextmgr"
	"nekocode/bot/provider/types"
)

func TestHistoryPageDurablePendingToolBoundaries(t *testing.T) {
	history := []types.Message{
		{Role: "user", Content: "1"},
		{Role: "assistant", Content: "2", ToolCalls: []types.ToolCall{{ID: "a", Function: types.FunctionCall{Name: "shell", Arguments: "{}"}}}},
		{Role: "tool", Content: "3", ToolCallID: "a"},
		{Role: "tool", Content: "4", ToolCallID: "a"},
		{Role: "assistant", Content: "5", ToolCalls: []types.ToolCall{{ID: "b", Function: types.FunctionCall{Name: "shell", Arguments: "{}"}}}},
		{Role: "tool", Content: "6", ToolCallID: "b"},
		{Role: "assistant", Content: "7"},
	}
	for split := 0; split <= len(history); split++ {
		t.Run(fmt.Sprintf("split%d", split), func(t *testing.T) {
			b := newPersistTestBot(t)
			if split > 0 {
				b.ctxMgr.Restore(contextmgr.ManagerSnapshot{Messages: history[:split], Transcript: history[:split]})
				if err := b.saveSession(); err != nil {
					t.Fatal(err)
				}
			}
			b.ctxMgr.Restore(contextmgr.ManagerSnapshot{Messages: history, Transcript: history[split:], TranscriptBase: split, TranscriptLazy: true})
			for end := 1; end <= len(history); end++ {
				for limit := 1; limit <= 4; limit++ {
					p, err := b.HistoryPage(end+1, limit, 1<<20)
					if err != nil {
						t.Fatalf("end%d limit%d: %v", end, limit, err)
					}
					first := max(1, end-limit+1)
					if p.Before != first || p.End != end {
						t.Fatalf("end%d limit%d wrong cursor: %+v", end, limit, p)
					}
					raw := p.Messages
					if history[first-1].Role == "tool" {
						if len(raw) == 0 || raw[0].Role != "assistant" || len(raw[0].ToolCalls) != 1 || raw[0].ToolCalls[0].ID != history[first-1].ToolCallID {
							t.Fatalf("end%d limit%d missing owner: %+v", end, limit, p)
						}
						raw = raw[1:]
					}
					if len(raw) != end-first+1 {
						t.Fatalf("end%d limit%d wrong count: %+v", end, limit, p)
					}
					for i, m := range raw {
						if m.Content != history[first+i-1].Content {
							t.Fatalf("end%d limit%d wrong content: %+v", end, limit, p)
						}
					}
				}
			}
		})
	}
}
