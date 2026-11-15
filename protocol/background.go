package protocol

import (
	"context"
	"errors"
)

// ErrBackgroundResumeDeliveryPending marks a safe lifecycle rejection. A task
// cannot be resumed in the same coordinator turn until its completion receipt
// has been delivered.
var ErrBackgroundResumeDeliveryPending = errors.New("background task notification is pending")

// BackgroundWork is a host-prepared execution. Model arguments never supply
// lifecycle callbacks or authority. Runtime owns the submitted execution.
type BackgroundWork struct {
	Isolation, SchedulingNote string
	// batch groups ordinary complete/continue handoffs until their stage settles.
	// Errors, blocked/decision outcomes and interaction waits bypass batching.
	// Legacy reports retain conservative coverage-based delivery. Empty is immediate.
	ReviewPolicy string
	After        []string
	Prompt       string
	// UpdateSequenceFloor comes from canonical child-session metadata on resume.
	UpdateSequenceFloor                               uint64
	ID, SessionID, GroupID, Title, Profile, Workspace string
	ReadOnly                                          bool
	// Run receives the durably admitted identity in ExecutionFromContext(ctx).
	// Executors retain its AttemptID when creating their own attempt metadata.
	Run     func(context.Context, BackgroundHost) (BackgroundResult, error)
	Release func()
}

type BackgroundUsage struct {
	TotalTokens, CacheHitTokens, CacheMissTokens, ToolUseCount int
	DurationMs                                                 int64
}
type BackgroundUsageSink interface{ Usage(BackgroundUsage) error }

type BackgroundHost interface {
	Step(StepEvent)
	Confirm(ConfirmRequest) ConfirmReply
	Ask(QuestionRequest) QuestionReply
}

// BackgroundInputSource supplies explicit user updates at model-turn boundaries.
// It is optional so existing hosts keep their execution contract.
type BackgroundUpdate struct {
	Sequence uint64
	Text     string
}

type BackgroundInputSource interface {
	ApplyUpdates(func([]BackgroundUpdate) error) error
}

type BackgroundReceipt struct {
	GroupID string `json:"plan_id,omitempty"`
	ID      string `json:"id"`
	Status  string `json:"status"`
}

type BackgroundSubmitter interface {
	SubmitBackground(context.Context, BackgroundWork) (BackgroundReceipt, error)
	CancelBackground(context.Context, string) error
}

// BackgroundPlanStatus is an optional trusted, demand-scoped activity query.
type BackgroundPlanStatus interface {
	PendingBackgroundPlan(groupID string) int
}

type backgroundKey struct{}
type coordinationKey struct{}

type Coordination struct {
	PendingTasks   int
	PendingReviews int
	GroupID        string
	Notification   bool
}

func WithBackgroundSubmitter(ctx context.Context, host BackgroundSubmitter) context.Context {
	return context.WithValue(ctx, backgroundKey{}, host)
}
func BackgroundSubmitterFromContext(ctx context.Context) (BackgroundSubmitter, bool) {
	host, ok := ctx.Value(backgroundKey{}).(BackgroundSubmitter)
	return host, ok
}
func WithCoordination(ctx context.Context, c Coordination) context.Context {
	return context.WithValue(ctx, coordinationKey{}, c)
}
func CoordinationFromContext(ctx context.Context) Coordination {
	c, _ := ctx.Value(coordinationKey{}).(Coordination)
	return c
}

// BackgroundResult is submitted once when execution ends. Review, when present,
// is the complete report; Text is used only by text-only executors.
type BackgroundResult struct {
	Text   string      `json:"text,omitempty"`
	Review *TaskReview `json:"review,omitempty"`
}

// BackgroundResultReader reads committed reports within the current parent session.
// Only legacy or absent tasks permit fallback to child-session results; active
// work and failed canonical commits return distinct errors.
var ErrBackgroundResultNotFound = errors.New("background result not found")

type BackgroundResultReader interface {
	ReadBackgroundResult(context.Context, string, ResultQuery) (ResultPage, error)
}

type backgroundResultReaderKey struct{}

func WithBackgroundResultReader(ctx context.Context, reader BackgroundResultReader) context.Context {
	return context.WithValue(ctx, backgroundResultReaderKey{}, reader)
}
func BackgroundResultReaderFromContext(ctx context.Context) (BackgroundResultReader, bool) {
	reader, ok := ctx.Value(backgroundResultReaderKey{}).(BackgroundResultReader)
	return reader, ok
}
