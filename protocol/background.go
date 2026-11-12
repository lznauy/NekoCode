package protocol

import "context"

// BackgroundWork is a host-prepared execution. Model arguments never supply
// lifecycle callbacks or authority. Runtime owns the submitted execution.
type BackgroundWork struct {
	Isolation, SchedulingNote string
	After                     []string
	Prompt                    string
	// UpdateSequenceFloor comes from canonical child-session metadata on resume.
	UpdateSequenceFloor                               uint64
	ID, SessionID, GroupID, Title, Profile, Workspace string
	ReadOnly                                          bool
	Run                                               func(context.Context, BackgroundHost) (string, error)
	Release                                           func()
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
	PendingTasks int
	GroupID      string
	Notification bool
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
