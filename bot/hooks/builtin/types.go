package builtin

type Point string

const (
	PreTurn    Point = "pre_turn"
	PreToolUse Point = "pre_tool_use"
	PostTool   Point = "post_tool"
	PostTurn   Point = "post_turn"
)

type StopReason string

const StopFormatError StopReason = "format_error"

type State interface {
	Get(key string) int64
	Set(key string, value int64)
	Flag(key string) bool
	GetStr(key string) string
	ToolName() string
	ToolArgs() map[string]any
}

type Hint struct {
	Type     string
	Severity string
	Content  string
}

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

type StatePatch struct {
	Ints map[string]int64
}

type Hook struct {
	Name  string
	Point Point
	On    func(State) *Result
}
