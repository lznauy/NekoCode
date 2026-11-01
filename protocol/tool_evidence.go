package protocol

// ProcessObservation is a sampled process fact, independent of tool errors.
// Empty State means no observation. ExitCodeKnown is true only for a normal
// exit. OutputComplete concerns captured bytes before text normalization.
type ProcessObservation struct {
	State          string `json:"state,omitempty"`
	ExitCode       int    `json:"exit_code,omitempty"`
	ExitCodeKnown  bool   `json:"exit_code_known,omitempty"`
	OutputComplete bool   `json:"output_complete,omitempty"`
}

// ExecutionReceipt records one physical tool-function invocation. Success for
// a tool without process facts means a nil tool error, never business success.
// Hashes identify boundary values; they are not permissions or signatures.
type ExecutionReceipt struct {
	ID            string             `json:"id"`
	Execution     ExecutionIdentity  `json:"execution"`
	CallID        string             `json:"call_id,omitempty"`
	Tool          string             `json:"tool"`
	ArgumentsHash string             `json:"arguments_hash,omitempty"`
	StartedAt     int64              `json:"started_at"`
	EndedAt       int64              `json:"ended_at"`
	Status        string             `json:"status"`
	OutputHash    string             `json:"output_hash"`
	ErrorHash     string             `json:"error_hash,omitempty"`
	RawComplete   bool               `json:"raw_complete"`
	Process       ProcessObservation `json:"process,omitzero"`
	Artifact      ToolOutputArtifact `json:"artifact,omitzero"`
}

// ToolEvidence is stored once on the authoritative tool transcript record.
// ReturnedHash identifies the executor delivery; VisibleHash identifies the
// initial context admission, not provider receipt or subsequent re-projection.
type ToolEvidence struct {
	Schema             int                `json:"schema"`
	Executions         []ExecutionReceipt `json:"executions"`
	ReturnedHash       string             `json:"returned_hash"`
	VisibleHash        string             `json:"visible_hash,omitempty"`
	Normalized         bool               `json:"normalized,omitempty"`
	Bounded            bool               `json:"bounded,omitempty"`
	PreservationFailed bool               `json:"preservation_failed,omitempty"`
	Admission          string             `json:"admission,omitempty"`
}
