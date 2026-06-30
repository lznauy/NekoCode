package hooks

type HookPoint string

const (
	PreTurn         HookPoint = "pre_turn"
	PreModelRequest HookPoint = "pre_model_request"
	PreToolUse      HookPoint = "pre_tool_use"
	PostToolUse     HookPoint = "post_tool_use" // per-tool (declarative hooks)
	PostTool        HookPoint = "post_tool"     // batch (builtin hooks)
	PostTurn        HookPoint = "post_turn"
	UserSubmit      HookPoint = "user_submit"
	Stop            HookPoint = "stop"
)

type Hint struct {
	Type     string
	Severity string
	Content  string
}

type StopReason string

const (
	StopFormatError StopReason = "format_error"
	StopInterrupted StopReason = "interrupted"
	StopCompleted   StopReason = "completed"
)

func (s StopReason) String() string { return string(s) }

type Result struct {
	Hint        *Hint
	Stop        *StopReason
	BlockTool   *BlockTool
	RequireTool *RequireTool
	BlockFinal  *BlockFinal
	StatePatch  *StatePatch
}

type BlockTool struct {
	Tool   string
	Reason string
}

type RequireTool struct {
	Tool   string
	Reason string
}

type BlockFinal struct {
	Reason string
}

type Hook struct {
	Name  string
	Point HookPoint
	On    func(s *Snapshot) *Result
}
