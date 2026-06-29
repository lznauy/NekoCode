package budget

import "nekocode/bot/governance"

const (
	MaxScore      = 200
	editRestore   = 60
	readCost      = 5
	grepCost      = 3
	webSearchCost = 3
	webFetchCost  = 8
	taskCost      = 12
	trivialCost   = 2
)

// ExplorationTracker implements a decay-score mechanism:
// starts at 200, tools deduct, edits restore.
// When score <= 0, forced precipitation is triggered via PreTurn hook.
type ExplorationTracker struct {
	Score int
}

// NewExplorationTracker creates a fresh tracker at max score.
func NewExplorationTracker() *ExplorationTracker {
	return &ExplorationTracker{Score: MaxScore}
}

// Record updates the exploration budget based on the tool called.
func (t *ExplorationTracker) Record(toolName string) {
	t.RecordCall(toolName, nil)
}

func (t *ExplorationTracker) RecordCall(toolName string, args map[string]any) {
	sem := governance.ClassifyToolCall(toolName, args)
	if sem.Mutating {
		t.Score = min(t.Score+editRestore, MaxScore)
		return
	}
	if !sem.Exploratory {
		return
	}
	if cost, ok := toolCosts[toolName]; ok {
		t.deduct(cost)
	}
}

// Reset fully restores the exploration budget.
func (t *ExplorationTracker) Reset() {
	t.Score = MaxScore
}

// toolCosts maps exploration tools to their score deduction.
var toolCosts = map[string]int{
	"read":       readCost,
	"grep":       grepCost,
	"glob":       trivialCost,
	"list":       trivialCost,
	"bash":       grepCost, // bash can be exploratory (ls, cat, etc.)
	"web_search": webSearchCost,
	"web_fetch":  webFetchCost,
	"task":       taskCost,
}

func (t *ExplorationTracker) deduct(amount int) {
	t.Score = max(t.Score-amount, 0)
}
