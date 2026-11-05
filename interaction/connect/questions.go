package connect

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"

	controlruntime "nekocode/runtime"
)

// QuestionTracker tracks pending questions so text-based channels can offer
// /answer (and channels with buttons can resolve option picks). It is fed by
// the IntentQuestion / IntentQuestionResolved intents a sink receives.
type QuestionTracker struct {
	mu      sync.Mutex
	pending map[string]controlruntime.QuestionView
	lastID  string
}

func NewQuestionTracker() *QuestionTracker {
	return &QuestionTracker{pending: make(map[string]controlruntime.QuestionView)}
}

// Add records a pending question.
func (t *QuestionTracker) Add(view controlruntime.QuestionView) {
	if view.ID == "" {
		return
	}
	t.mu.Lock()
	t.pending[view.ID] = view
	t.lastID = view.ID
	t.mu.Unlock()
}

// Remove drops a question (resolved or dismissed). It is safe to call with
// unknown IDs.
func (t *QuestionTracker) Remove(id string) {
	t.mu.Lock()
	delete(t.pending, id)
	if t.lastID == id {
		t.lastID = ""
	}
	t.mu.Unlock()
}

// Clear drops all tracked questions when a connector's authorized identity
// changes. This prevents a newly paired user from targeting the prior user's
// implicit "last question" with /answer.
func (t *QuestionTracker) Clear() {
	t.mu.Lock()
	clear(t.pending)
	t.lastID = ""
	t.mu.Unlock()
}

// View returns the pending question view, for channels rendering options.
func (t *QuestionTracker) View(id string) (controlruntime.QuestionView, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	v, ok := t.pending[id]
	return v, ok
}

// LastID returns the most recently added still-pending question ID.
func (t *QuestionTracker) LastID() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastID
}

// BuildReply builds a reply from raw text; an empty questionID targets the
// most recent pending question. Multi-part questions take "|"-separated
// parts; option questions accept option numbers or (prefixes of) labels,
// comma-separated for multi-select.
func (t *QuestionTracker) BuildReply(questionID, raw string) (controlruntime.QuestionReply, string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	view, id, err := t.resolveLocked(questionID)
	if err != nil {
		return controlruntime.QuestionReply{}, "", err
	}
	reply, err := buildQuestionReply(view, raw)
	if err != nil {
		return controlruntime.QuestionReply{}, "", err
	}
	return reply, id, nil
}

// BuildOptionReply builds a reply for a single-select question from an
// option index.
func (t *QuestionTracker) BuildOptionReply(questionID string, optionIndex int) (controlruntime.QuestionReply, string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	view, id, err := t.resolveLocked(questionID)
	if err != nil {
		return controlruntime.QuestionReply{}, "", err
	}
	if len(view.Questions) != 1 {
		return controlruntime.QuestionReply{}, "", fmt.Errorf("question %s requires text answer", id)
	}
	item := view.Questions[0]
	if item.Multiple || optionIndex < 0 || optionIndex >= len(item.Options) {
		return controlruntime.QuestionReply{}, "", fmt.Errorf("question %s requires text answer", id)
	}
	return controlruntime.QuestionReply{Answers: [][]string{{item.Options[optionIndex].Label}}}, id, nil
}

// BuildMultiOptionReply builds a reply for a multi-select question from the
// selected option indices. At least one index is required.
func (t *QuestionTracker) BuildMultiOptionReply(questionID string, indices []int) (controlruntime.QuestionReply, string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	view, id, err := t.resolveLocked(questionID)
	if err != nil {
		return controlruntime.QuestionReply{}, "", err
	}
	if len(view.Questions) != 1 || !view.Questions[0].Multiple {
		return controlruntime.QuestionReply{}, "", fmt.Errorf("question %s is not a multi-select question", id)
	}
	item := view.Questions[0]
	var labels []string
	for i, opt := range item.Options {
		if slices.Contains(indices, i) {
			labels = append(labels, opt.Label)
		}
	}
	if len(labels) == 0 {
		return controlruntime.QuestionReply{}, "", fmt.Errorf("请至少选择一项")
	}
	return controlruntime.QuestionReply{Answers: [][]string{labels}}, id, nil
}

// Reject resolves the question ID (defaulting to the latest) for a dismiss.
func (t *QuestionTracker) Reject(questionID string) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, id, err := t.resolveLocked(questionID)
	return id, err
}

func (t *QuestionTracker) resolveLocked(questionID string) (controlruntime.QuestionView, string, error) {
	if strings.TrimSpace(questionID) == "" {
		questionID = t.lastID
	}
	view, ok := t.pending[questionID]
	if !ok {
		return controlruntime.QuestionView{}, "", fmt.Errorf("question %s is not pending", questionID)
	}
	return view, questionID, nil
}

func buildQuestionReply(qv controlruntime.QuestionView, raw string) (controlruntime.QuestionReply, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return controlruntime.QuestionReply{}, fmt.Errorf("empty answer")
	}
	parts := splitAnswerParts(raw, len(qv.Questions))
	answers := make([][]string, len(qv.Questions))
	for i, item := range qv.Questions {
		part := ""
		if i < len(parts) {
			part = parts[i]
		}
		parsed, err := parseQuestionAnswer(item, part)
		if err != nil {
			return controlruntime.QuestionReply{}, fmt.Errorf("question %d: %w", i+1, err)
		}
		answers[i] = parsed
	}
	return controlruntime.QuestionReply{Answers: answers}, nil
}

func splitAnswerParts(raw string, count int) []string {
	if count <= 1 {
		return []string{strings.TrimSpace(raw)}
	}
	parts := strings.Split(raw, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func parseQuestionAnswer(item controlruntime.QuestionItem, raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty answer")
	}
	if len(item.Options) == 0 {
		return []string{raw}, nil
	}
	var tokens []string
	if item.Multiple {
		tokens = strings.Split(raw, ",")
	} else {
		tokens = []string{raw}
	}
	answers := make([]string, 0, len(tokens))
	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		label, ok := matchQuestionOption(item.Options, token)
		if !ok {
			if item.Custom {
				answers = append(answers, token)
				continue
			}
			return nil, fmt.Errorf("unknown option %q", token)
		}
		answers = append(answers, label)
	}
	if len(answers) == 0 {
		return nil, fmt.Errorf("empty answer")
	}
	return answers, nil
}

func matchQuestionOption(options []controlruntime.QuestionOption, token string) (string, bool) {
	if idx, err := strconv.Atoi(token); err == nil {
		idx--
		if idx >= 0 && idx < len(options) {
			return options[idx].Label, true
		}
	}
	folded := strings.ToLower(token)
	// Exact labels take precedence over another option's prefix.
	for _, opt := range options {
		if folded == strings.ToLower(opt.Label) {
			return opt.Label, true
		}
	}
	var matched string
	matches := 0
	for _, opt := range options {
		if strings.HasPrefix(strings.ToLower(opt.Label), folded) {
			matched = opt.Label
			matches++
		}
	}
	if matches == 1 {
		return matched, true
	}
	return "", false
}

// ParseAnswerCommand splits "/answer [question-id] <answer>" into its parts;
// the ID is optional and defaults to the tracker's latest pending question.
func ParseAnswerCommand(text string) (questionID, answer string) {
	rest := strings.TrimSpace(strings.TrimPrefix(text, "/answer"))
	if rest == "" {
		return "", ""
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return "", ""
	}
	if strings.HasPrefix(fields[0], "q_") {
		return fields[0], strings.TrimSpace(strings.TrimPrefix(rest, fields[0]))
	}
	return "", rest
}
