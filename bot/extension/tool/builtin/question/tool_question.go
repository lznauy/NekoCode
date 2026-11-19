package question

import (
	"context"
	"fmt"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"strings"
	"sync"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/protocol"
)

type Tool struct {
	mu sync.RWMutex
	fn protocol.QuestionFunc
}

func NewTool() *Tool { return &Tool{} }

func (t *Tool) Name() string                                    { return "question" }
func (t *Tool) ExecutionMode(map[string]any) core.ExecutionMode { return core.ModeSequential }

func (t *Tool) Description() string {
	return "Ask the user structured questions during execution and wait for their answers. Use this when a decision, preference, or clarification is required before continuing."
}

func (t *Tool) Parameters() []core.Parameter {
	return []core.Parameter{
		{
			Name:        "questions",
			Type:        "array",
			Required:    true,
			Description: "One or more decisions that require user input.",
			Items: &core.Schema{
				Type: "object", Required: []string{"question"},
				Properties: map[string]core.Schema{
					"header":   {Type: "string", Description: "Short label for the decision."},
					"question": {Type: "string", Description: "Clear question whose answer is needed to continue."},
					"options": {
						Type: "array", Description: "Optional predefined choices.",
						Items: &core.Schema{
							Type: "object", Required: []string{"label"},
							Properties: map[string]core.Schema{
								"label":       {Type: "string"},
								"description": {Type: "string", Description: "Effect or tradeoff of this choice."},
							},
						},
					},
					"multiple": {Type: "boolean", Description: "Allow selecting more than one option; defaults to false."},
					"custom":   {Type: "boolean", Description: "Allow a free-form answer; defaults to true. If false, at least one valid option is required."},
				},
			},
		},
	}
}

func (t *Tool) SetQuestionFunc(fn protocol.QuestionFunc) {
	t.mu.Lock()
	t.fn = fn
	t.mu.Unlock()
}

func (t *Tool) questionFunc() protocol.QuestionFunc {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.fn
}

func (t *Tool) Execute(ctx context.Context, args map[string]any) (core.ToolOutput, error) {
	select {
	case <-ctx.Done():
		return core.ToolOutput{}, ctx.Err()
	default:
	}

	questions, err := parseQuestions(args)
	if err != nil {
		return core.ToolOutput{}, err
	}
	fn := t.questionFunc()
	if fn == nil {
		return core.ToolOutput{}, fmt.Errorf("question UI is not available")
	}

	req := protocol.NewQuestionRequest(questions)
	req.SubAgentID = taskbridge.TaskIDFromCtx(ctx)
	reply := fn(req)
	if err := ctx.Err(); err != nil {
		return core.ToolOutput{}, err
	}
	if reply.Rejected {
		return core.ToolOutput{}, fmt.Errorf("question rejected or closed without an answer; do not infer a user decision or repeat the unchanged request automatically")
	}
	output := core.ToolOutput{Text: formatAnswers(questions, reply.Answers)}
	if err := validateAnswers(questions, reply.Answers); err != nil {
		return output, err
	}
	output.Text += "\nContinue with these answers in mind."
	return output, nil
}

func parseQuestions(args map[string]any) ([]protocol.QuestionItem, error) {
	raw, ok := args["questions"].([]any)
	if !ok || len(raw) == 0 {
		return nil, fmt.Errorf("questions must be a non-empty array")
	}
	out := make([]protocol.QuestionItem, 0, len(raw))
	for i, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("questions[%d] must be an object", i)
		}
		q, _ := m["question"].(string)
		q = strings.TrimSpace(q)
		if q == "" {
			return nil, fmt.Errorf("questions[%d].question is required", i)
		}
		header, err := optionalText(m, "header")
		if err != nil {
			return nil, fmt.Errorf("questions[%d]: %w", i, err)
		}
		options, err := parseOptions(m["options"])
		if err != nil {
			return nil, fmt.Errorf("questions[%d]: %w", i, err)
		}
		multiple, err := optionalBool(m, "multiple", false)
		if err != nil {
			return nil, fmt.Errorf("questions[%d]: %w", i, err)
		}
		custom, err := optionalBool(m, "custom", true)
		if err != nil {
			return nil, fmt.Errorf("questions[%d]: %w", i, err)
		}
		if !custom && len(options) == 0 {
			return nil, fmt.Errorf("questions[%d] needs at least one option when custom=false; no question was shown", i)
		}
		out = append(out, protocol.QuestionItem{
			Header:   strings.TrimSpace(header),
			Question: q,
			Options:  options,
			Multiple: multiple,
			Custom:   custom,
		})
	}
	return out, nil
}

func parseOptions(raw any) ([]protocol.QuestionOption, error) {
	if raw == nil {
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("options must be an array of objects")
	}
	options := make([]protocol.QuestionOption, 0, len(items))
	seen := make(map[string]bool, len(items))
	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("options[%d] must be an object with a label", i)
		}
		label, _ := m["label"].(string)
		label = strings.TrimSpace(label)
		if label == "" {
			return nil, fmt.Errorf("options[%d].label must be a nonempty string", i)
		}
		if seen[label] {
			return nil, fmt.Errorf("options[%d] repeats label %q; use distinct labels", i, label)
		}
		seen[label] = true
		desc, err := optionalText(m, "description")
		if err != nil {
			return nil, fmt.Errorf("options[%d]: %w", i, err)
		}
		options = append(options, protocol.QuestionOption{Label: label, Description: desc})
	}
	return options, nil
}

func optionalText(args map[string]any, key string) (string, error) {
	raw, supplied := args[key]
	if !supplied {
		return "", nil
	}
	value, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", key)
	}
	return strings.TrimSpace(value), nil
}

func optionalBool(args map[string]any, key string, fallback bool) (bool, error) {
	raw, supplied := args[key]
	if !supplied {
		return fallback, nil
	}
	value, ok := raw.(bool)
	if !ok {
		return false, fmt.Errorf("%s must be a boolean", key)
	}
	return value, nil
}

func validateAnswers(questions []protocol.QuestionItem, answers [][]string) error {
	if len(answers) != len(questions) {
		return fmt.Errorf("question reply is incomplete or has an invalid answer count; do not infer decisions or automatically repeat unchanged questions")
	}
	for i, q := range questions {
		if len(answers[i]) == 0 {
			return fmt.Errorf("questions[%d] was unanswered; do not infer a decision or automatically repeat the unchanged question", i)
		}
		if !q.Multiple && len(answers[i]) != 1 {
			return fmt.Errorf("questions[%d] accepts only one answer", i)
		}
		seen := make(map[string]bool, len(answers[i]))
		for _, answer := range answers[i] {
			answer = strings.TrimSpace(answer)
			if answer == "" || seen[answer] {
				return fmt.Errorf("questions[%d] has an empty or duplicate answer; do not infer a decision", i)
			}
			seen[answer] = true
			if !q.Custom {
				found := false
				for _, option := range q.Options {
					if answer == option.Label {
						found = true
						break
					}
				}
				if !found {
					return fmt.Errorf("questions[%d] answer must match a supplied option", i)
				}
			}
		}
	}
	return nil
}

func formatAnswers(questions []protocol.QuestionItem, answers [][]string) string {
	var b strings.Builder
	b.WriteString("Question responses:\n")
	for i, q := range questions {
		answer := "Unanswered"
		if i < len(answers) && len(answers[i]) > 0 {
			answer = strings.Join(answers[i], ", ")
		}
		fmt.Fprintf(&b, "- %s: %s\n", q.Question, answer)
	}
	return strings.TrimSpace(b.String())
}
