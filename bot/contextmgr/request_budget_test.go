package contextmgr

import (
	"context"
	"errors"
	"strings"
	"testing"

	"nekocode/bot/provider/types"
)

func requireRequestBudgetError(t *testing.T, err error) *RequestBudgetError {
	t.Helper()
	var budget *RequestBudgetError
	if !errors.As(err, &budget) {
		t.Fatalf("want budget error, got %v", err)
	}
	return budget
}

func TestPrepareRequestCountsFinalRequestParts(t *testing.T) {
	for _, part := range []string{"tools", "runtime", "hint", "extra", "output", "calibration"} {
		t.Run(part, func(t *testing.T) {
			cfg := Config{ContextWindow: 2000, SystemPrompt: "small"}
			request := ModelRequest{OutputTokens: 100}
			huge := strings.Repeat("x", 10000)
			switch part {
			case "tools":
				request.Tools = []types.ToolDef{{Type: "function", Function: types.FunctionDef{Name: "search", Description: huge}}}
			case "runtime":
				cfg.RuntimePrompt = func() string { return huge }
			case "hint":
				request.PolicyHints = huge
			case "extra":
				request.ExtraMessages = []types.Message{{Role: "user", Content: huge}}
			case "output":
				request.OutputTokens = 2100
			}
			m := New(cfg)
			if part == "calibration" {
				m.RecordModelUsage(types.StreamUsage{PromptTokens: 2100})
			}
			messages, err := m.PrepareRequest(context.Background(), request)
			budget := requireRequestBudgetError(t, err)
			if messages != nil || budget.Total() <= budget.Window {
				t.Fatal("oversized request escaped")
			}
			if part == "tools" && budget.Tools < 2000 {
				t.Fatal("tool definitions omitted")
			}
			if part == "calibration" && budget.Calibration == 0 {
				t.Fatal("provider calibration ignored")
			}
			if part == "extra" && len(m.Snapshot().Messages) != 0 {
				t.Fatal("transient content persisted")
			}
		})
	}
}

func TestPrepareRequestCompactsOnceAndRebuilds(t *testing.T) {
	for _, summaryFits := range []bool{true, false} {
		calls := 0
		summary := "<summary>older work</summary>"
		if !summaryFits {
			summary = "<summary>" + strings.Repeat("x", 20000) + "</summary>"
		}
		m := New(Config{ContextWindow: 4000, Summarizer: func([]types.Message, string) (string, error) { calls++; return summary, nil }})
		for i := 0; i < 8; i++ {
			m.Add("user", strings.Repeat("q", 800))
			m.Add("assistant", strings.Repeat("a", 800))
		}
		request := ModelRequest{OutputTokens: 1500, ExtraMessages: []types.Message{{Role: "user", Content: "request-only workflow"}}}
		messages, err := m.PrepareRequest(context.Background(), request)
		if calls != 1 {
			t.Fatalf("summary calls=%d, want one", calls)
		}
		if !summaryFits {
			requireRequestBudgetError(t, err)
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, msg := range messages {
			if msg.Content == "request-only workflow" {
				count++
			}
		}
		if m.Report().PrefixTurn.Requests != 1 {
			t.Fatal("pre-compaction draft was counted as a model request")
		}
		if count != 1 || len(m.Snapshot().Messages) >= 16 {
			t.Fatal("request not rebuilt correctly")
		}
		for _, msg := range m.Snapshot().Messages {
			if msg.Content == "request-only workflow" {
				t.Fatal("extra message persisted")
			}
		}
	}
}

func TestPrepareRequestChecksImageTokensForActiveModel(t *testing.T) {
	m := New(Config{ContextWindow: 2000})
	m.ConfigureModel(ModelContext{Window: 2000, Vision: true})
	m.AddUser("image", []types.MessageImage{{Width: 1000, Height: 1000}})
	_, err := m.PrepareRequest(context.Background(), ModelRequest{OutputTokens: 1000})
	requireRequestBudgetError(t, err)
	m.ConfigureModel(ModelContext{Window: 2000, Vision: false})
	if _, err := m.PrepareRequest(context.Background(), ModelRequest{OutputTokens: 1000}); err != nil {
		t.Fatal("non-vision request charged for image", err)
	}
}

func TestPrepareRequestExactBoundary(t *testing.T) {
	m := New(Config{ContextWindow: 2000, SystemPrompt: "small"})
	messages := m.BuildRequest(ModelRequest{})
	budget, err := estimateRequestBudget(messages, nil, 2000, 100, types.ReasoningSettings{}, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	output := 100 + 2000 - budget.Total()
	if _, err := m.PrepareRequest(context.Background(), ModelRequest{OutputTokens: output}); err != nil {
		t.Fatal(err)
	}
	_, err = m.PrepareRequest(context.Background(), ModelRequest{OutputTokens: output + 1})
	requireRequestBudgetError(t, err)
}

func TestPrepareRequestCancellationDuringCompaction(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := New(Config{ContextWindow: 4000, Summarizer: func([]types.Message, string) (string, error) { cancel(); return "<summary>older work</summary>", nil }})
	for i := 0; i < 8; i++ {
		m.Add("user", strings.Repeat("q", 800))
		m.Add("assistant", strings.Repeat("a", 800))
	}
	_, err := m.PrepareRequest(ctx, ModelRequest{OutputTokens: 1500})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation: %v", err)
	}
	var budget *RequestBudgetError
	if errors.As(err, &budget) {
		t.Fatal("cancellation must not be tagged as a fatal budget error")
	}
}

type budgetSummaryClient struct {
	usageTestClient
	calls int
}

func (c *budgetSummaryClient) GetMaxTokens() int { return 3000 }
func (c *budgetSummaryClient) ChatStream(ctx context.Context, msgs []types.Message, tools []types.ToolDef) (<-chan types.StreamToken, <-chan error) {
	c.calls++
	return c.usageTestClient.ChatStream(ctx, msgs, tools)
}
func TestSummaryRequestBudgetStopsBeforeProviderCall(t *testing.T) {
	client := &budgetSummaryClient{}
	m := New(Config{CompactionModel: client})
	m.ConfigureModel(ModelContext{CompactionModel: client, CompactionWindow: 2000})
	_, err := m.streamingSummarizer(context.Background(), client, nil)([]types.Message{{Role: "user", Content: "small"}}, "")
	budget := requireRequestBudgetError(t, err)
	if budget.Output != 3000 || client.calls != 0 {
		t.Fatalf("summary request escaped budget: calls=%d output=%d", client.calls, budget.Output)
	}
}

func TestRequestBudgetDraftsDoNotCountAsRequests(t *testing.T) {
	m := New(Config{ContextWindow: 2000, SystemPrompt: "small"})
	request := ModelRequest{OutputTokens: 100}
	if _, err := m.PrepareRequest(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	baseline := m.Snapshot().PrefixBaseline
	rejected := request
	rejected.Tools = []types.ToolDef{{Type: "function", Function: types.FunctionDef{Name: "large", Description: strings.Repeat("x", 20000)}}}
	if _, err := m.PrepareRequest(context.Background(), rejected); err == nil {
		t.Fatal("oversized draft accepted")
	}
	if m.Report().PrefixTurn.Requests != 1 || m.Snapshot().PrefixBaseline != baseline {
		t.Fatal("rejected draft polluted request accounting")
	}
	if _, err := m.PrepareRequest(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if m.Report().PrefixTurn.Requests != 2 {
		t.Fatal("accepted request was not counted")
	}
}

func TestSummaryBudgetUsesFormattedRequest(t *testing.T) {
	history := []types.Message{{Role: "tool", Content: strings.Repeat("x", 100000)}}
	request, err := prepareSummaryRequest(history, "", 10000, &budgetSummaryClient{})
	if err != nil {
		t.Fatal("formatted summary should fit despite a large raw result", err)
	}
	if len(request) != 2 || len(request[1].Content) >= len(history[0].Content) {
		t.Fatal("summary request was not formatted")
	}
}
