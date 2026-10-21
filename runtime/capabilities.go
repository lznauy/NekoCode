package runtime

import "context"

// Consumer-facing capability groups keep transport contracts composable.
// ServiceGroups supplies application callbacks at the composition root;
// consumers depend only on the capabilities they actually use.
type RunControl interface {
	StartRun(context.Context, Input) (RunID, error)
	CancelRun(context.Context, RunID) error
}

type RunEvents interface {
	Events(context.Context, EventFilter) (<-chan Event, error)
}

type HumanInteraction interface {
	DecideApproval(context.Context, string, ApprovalDecision) error
	AnswerQuestion(context.Context, string, QuestionReply) error
}

type SessionReader interface {
	CurrentSessionID() string
	ListSessions() []SessionMeta
	SessionMessages() []DisplayMessage
}

type SessionManagement interface {
	SessionReader
	NewSession() (SessionMeta, error)
	ResumeSession(string) error
	DeleteSession(string) error
}

type ModelReader interface {
	ModelOptions() ([]ModelOption, string)
	CurrentModel() ModelSelection
}

type ObservationReader interface {
	ObservedOverview() ObserveOverview
	ObservedRuns(ObservedRunFilter) []ObservedRunSummary
	ObservedRun(RunID) (ObservedRunDetail, bool)
	ObservedSourceKinds() []string
	ObservedToolCatalog() []string
	Prometheus() string
}

var (
	_ RunControl        = (*Runtime)(nil)
	_ RunEvents         = (*Runtime)(nil)
	_ HumanInteraction  = (*Runtime)(nil)
	_ SessionManagement = (*Runtime)(nil)
	_ ModelReader       = (*Runtime)(nil)
	_ ObservationReader = (*Runtime)(nil)
)
