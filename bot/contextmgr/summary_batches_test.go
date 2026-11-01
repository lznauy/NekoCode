package contextmgr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

func summaryTestHistory(old string) []types.Message {
	history := []types.Message{{Role: "user", Content: old}, {Role: "assistant", Content: "old reply"}}
	for i := 0; i < 3; i++ {
		history = append(history, types.Message{Role: "user", Content: fmt.Sprintf("recent %d", i)}, types.Message{Role: "assistant", Content: "recent reply"})
	}
	return history
}

func TestSummaryBatchesCoverFullSourceAndArchive(t *testing.T) {
	source := types.Message{Role: "assistant", ToolCalls: []types.ToolCall{{ID: "call", Function: types.FunctionCall{Name: "shell", Arguments: strings.Repeat("参数\\\"\n", 6000) + "TAIL"}}}}
	archive := strings.Repeat("old archive ", 1500) + "ARCHIVE_TAIL"
	rc := newReplacementCompactor(nil, 80)
	rc.modelWindow = 16000
	batches, err := rc.planSummaryBatches([]types.Message{source}, archive)
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) < 2 || len(batches) > maxSummaryCalls {
		t.Fatalf("batches = %d", len(batches))
	}
	var reconstructed []string
	current := ""
	lastID, part := "", 0
	for _, batch := range batches {
		if _, err := prepareSummaryRequest(batch.messages, strings.Repeat("s", rc.summaryOutput()*4), rc.modelWindow, nil); err != nil {
			t.Fatal("reserved carry does not fit", err)
		}
		for _, msg := range batch.messages {
			var fragment struct {
				Source string `json:"source_sha256"`
				Part   int    `json:"part"`
				Final  bool   `json:"final"`
				Data   string `json:"data"`
			}
			if json.Unmarshal([]byte(msg.Content), &fragment) == nil && fragment.Source != "" {
				if lastID != fragment.Source {
					if current != "" {
						t.Fatal("source changed before final fragment")
					}
					lastID, part = fragment.Source, 0
				}
				part++
				if fragment.Part != part {
					t.Fatal("fragment sequence gap")
				}
				current += fragment.Data
				if fragment.Final {
					reconstructed = append(reconstructed, current)
					current = ""
				}
			} else {
				reconstructed = append(reconstructed, formatSummaryMessages([]types.Message{msg}))
			}
		}
	}
	want := []string{formatSummaryMessages([]types.Message{{Role: "archive", Content: archive}}), formatSummaryMessages([]types.Message{source})}
	if current != "" || !reflect.DeepEqual(reconstructed, want) {
		t.Fatal("source fragments did not reconstruct exact ordered records")
	}
	calls := 0
	_, err = rc.summarizeBatches([]types.Message{source}, archive, func(messages []types.Message, prev string) (string, error) {
		if calls > 0 && prev != fmt.Sprintf("batch %d", calls) {
			t.Fatalf("previous batch missing: %q", prev)
		}
		calls++
		return fmt.Sprintf("<summary>batch %d</summary>", calls), nil
	})
	if err != nil || calls != len(batches) {
		t.Fatalf("batch execution = %d, %v", calls, err)
	}
}

func TestSummaryBatchFailureIsAtomic(t *testing.T) {
	for _, mode := range []string{"error", "cancel", "malformed", "steer"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			var m *Manager
			m = New(Config{ContextWindow: 64000, Summarizer: func([]types.Message, string) (string, error) {
				calls++
				if calls == 2 {
					switch mode {
					case "error":
						return "", fmt.Errorf("batch failure")
					case "cancel":
						cancel()
					case "malformed":
						return "<summary>unfinished", nil
					case "steer":
						m.Add("user", "steered request")
					}
				}
				return "<summary>intermediate</summary>", nil
			}})
			m.state.compressor.modelWindow = 16000
			for _, msg := range summaryTestHistory(strings.Repeat("source ", 11000)) {
				m.Add(msg.Role, msg.Content)
			}
			before := m.Snapshot()
			applied, err := m.Summarize(ctx)
			if applied || err == nil || calls < 2 {
				t.Fatalf("failure = %v, %v; calls=%d", applied, err, calls)
			}
			if mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation: %v", err)
			}
			after := m.Snapshot()
			if mode == "steer" {
				if after.Messages[len(after.Messages)-1].Content != "steered request" || after.Archive != before.Archive {
					t.Fatal("lost concurrent input")
				}
			} else if !reflect.DeepEqual(before, after) {
				t.Fatal("failed batch mutated context")
			}
		})
	}
}

func TestSummaryBatchLimitPreflightDoesNotCallModel(t *testing.T) {
	calls := 0
	rc := newReplacementCompactor(func([]types.Message, string) (string, error) { calls++; return "<summary>ok</summary>", nil }, 80)
	rc.modelWindow = 16000
	_, _, _, err := rc.summarize(summaryTestHistory(strings.Repeat("data ", 100000)), "", 64000, nil)
	if err == nil || calls != 0 || !strings.Contains(err.Error(), "8") {
		t.Fatalf("preflight = calls %d, error %v", calls, err)
	}
}

type batchUsageClient struct {
	usageTestClient
	calls      int
	retryEmpty bool
	requests   [][]types.Message
}

func (c *batchUsageClient) ChatStream(ctx context.Context, messages []types.Message, tools []types.ToolDef) (<-chan types.StreamToken, <-chan error) {
	c.calls++
	c.requests = append(c.requests, append([]types.Message(nil), messages...))
	content := "<summary>carry summary</summary>"
	if c.retryEmpty && c.calls%2 == 1 {
		content = ""
	}
	response := usageTestClient{response: &types.Response{Choices: []types.Choice{{Message: types.Message{Content: content}}}, Usage: types.StreamUsage{PromptTokens: 10, CompletionTokens: 1}}}
	return response.ChatStream(ctx, messages, tools)
}

func TestCompactionBatchesCountPhysicalCallsAndUsageOnce(t *testing.T) {
	for _, retryEmpty := range []bool{false, true} {
		t.Run(fmt.Sprint(retryEmpty), func(t *testing.T) {
			client := &batchUsageClient{retryEmpty: retryEmpty}
			m := New(Config{ContextWindow: 128000, CompactionModel: client})
			m.ConfigureModel(ModelContext{Window: 128000, CompactionModel: client, CompactionWindow: 16000})
			for _, msg := range summaryTestHistory(strings.Repeat("source ", 20000) + "SOURCE_TAIL") {
				m.Add(msg.Role, msg.Content)
			}
			before := m.Snapshot()
			recorded, promptTokens, started, completed, failed := 0, 0, 0, 0, 0
			m.SetLLMUsageRecorder(func(usage types.StreamUsage) { recorded++; promptTokens += usage.PromptTokens })
			m.SetCompactionObserver(func(event protocol.CompactionEvent) {
				switch event.Status {
				case protocol.CompactionStarted:
					started++
				case protocol.CompactionCompleted:
					completed++
				case protocol.CompactionFailed:
					failed++
				}
			})
			applied, err := m.Summarize(context.Background())
			if recorded != client.calls || promptTokens != client.calls*10 {
				t.Fatalf("usage duplicated/lost: calls=%d recorded=%d prompt=%d", client.calls, recorded, promptTokens)
			}
			if started != 1 {
				t.Fatalf("started events = %d", started)
			}
			if retryEmpty {
				if applied || err == nil || client.calls != maxSummaryCalls || failed != 1 {
					t.Fatalf("retry ceiling = applied %v err %v calls %d failed %d", applied, err, client.calls, failed)
				}
				if !reflect.DeepEqual(before, m.Snapshot()) {
					t.Fatal("call ceiling committed partial summary")
				}
			} else {
				if !applied || err != nil || client.calls < 2 || client.calls > maxSummaryCalls || completed != 1 {
					t.Fatalf("batch calls = %d, applied %v, error %v", client.calls, applied, err)
				}
				joined := ""
				for _, request := range client.requests {
					if request[0].Role != "system" || len(request) != 2 || request[1].Role != "user" {
						t.Fatal("history roles leaked to wire")
					}
					joined += request[1].Content
				}
				if !strings.Contains(joined, "SOURCE_TAIL") {
					t.Fatal("actual model requests missed source tail")
				}
			}
		})
	}
}
