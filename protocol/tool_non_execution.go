package protocol

// ToolNonExecution records a host decision before Tool.Execute was invoked.
// It is not an ExecutionReceipt, permission grant, or proof of external state.
// The existing tool result carries display text; only its hash is stored here.
type ToolNonExecution struct {
	Schema     int               `json:"schema"`
	ID         string            `json:"id"`
	Execution  ExecutionIdentity `json:"execution"`
	CallID     string            `json:"call_id,omitempty"`
	Tool       string            `json:"tool"`
	Stage      string            `json:"stage"`
	Outcome    string            `json:"outcome"`
	ReasonHash string            `json:"reason_hash"`
	RecordedAt int64             `json:"recorded_at"`
}
