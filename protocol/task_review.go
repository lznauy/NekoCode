package protocol

// TaskReview is worker-reported evidence, not a host-certified verdict. The
// canonical full report remains in the task attempt; this is its structured
// handoff for coordination. It never grants execution or file permissions.
type TaskReview struct {
	TaskReport
	// Workspace metadata is supplied by the host, independently of worker claims.
	WorkspaceKind string `json:"workspace_kind,omitempty"`
	WorkspacePath string `json:"workspace_path,omitempty"`
	WorkspaceBase string `json:"workspace_base,omitempty"`
	SourceHead    string `json:"source_head,omitempty"`
}

// TaskReport is the worker-owned handoff shared by execution and coordination.
type TaskReport struct {
	// Outcome and NextAction describe scheduling needs, never task-specific quality.
	// Empty outcome is the legacy completion contract.
	Outcome    string `json:"outcome,omitempty"`
	NextAction string `json:"next_action,omitempty"`

	Summary          string            `json:"summary"`
	Evidence         []string          `json:"evidence"`
	EvidenceIDs      []string          `json:"evidence_ids,omitempty"`
	Files            []string          `json:"files"`
	Verification     string            `json:"verification"`
	Checks           []TaskReviewCheck `json:"checks"`
	VerificationGaps []string          `json:"verification_gaps,omitempty"`
	Unfinished       []string          `json:"unfinished"`
	Risks            []string          `json:"risks"`
}

type TaskReviewCheck struct {
	Name        string   `json:"name"`
	Method      string   `json:"method,omitempty"`
	Status      string   `json:"status"`
	Evidence    string   `json:"evidence"`
	Entrypoint  string   `json:"entrypoint"`
	Scope       string   `json:"scope"`
	Limitations []string `json:"limitations"`
}
