package protocol

type ResultQuery struct {
	ID, AttemptID, Ref string
	Offset, Limit      int
}

type ResultPage struct {
	WorkspaceBase string `json:"workspace_base,omitempty"`
	SourceHead    string `json:"source_head,omitempty"`
	WorkspaceKind string `json:"workspace_kind,omitempty"`
	Isolation     string `json:"isolation,omitempty"`
	ID            string `json:"id"`
	AttemptID     string `json:"attempt_id"`
	Ref           string `json:"result_ref"`
	Status        string `json:"status"`
	Content       string `json:"content"`
	Offset        int    `json:"offset"`
	NextOffset    int    `json:"next_offset"`
	Total         int    `json:"total"`
	HasMore       bool   `json:"has_more"`
}

// ResultNotReadyError distinguishes active work from a failed result read.
type ResultNotReadyError struct{ Status string }

func (e *ResultNotReadyError) Error() string {
	return "task result is not final (" + e.Status + "); wait for completion notification"
}
