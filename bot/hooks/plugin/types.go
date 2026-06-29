package plugin

type Point string

const (
	PreTurn     Point = "pre_turn"
	PreToolUse  Point = "pre_tool_use"
	PostToolUse Point = "post_tool_use"
	UserSubmit  Point = "user_submit"
	Stop        Point = "stop"
)

type Event struct {
	Tool  string
	Error bool
}

type Hint struct {
	Type     string
	Severity string
	Content  string
}

type Result struct {
	Hint        *Hint
	Stop        *StopResult
	BlockTool   *BlockToolResult
	RequireTool *RequireToolResult
	BlockFinal  *BlockFinalResult
	StatePatch  *StatePatchResult
}

type StopResult struct {
	Reason string
}

type BlockToolResult struct {
	Tool   string
	Reason string
}

type RequireToolResult struct {
	Tool   string
	Reason string
}

type BlockFinalResult struct {
	Reason string
}

type StatePatchResult struct {
	Ints    map[string]int64  `json:"ints"`
	Strings map[string]string `json:"strings"`
}

type Hook struct {
	Name  string
	Point Point
	Once  bool
	On    func(Event) *Result
}
