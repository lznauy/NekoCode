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
	Hint *Hint
}

type Hook struct {
	Name  string
	Point Point
	Once  bool
	On    func(Event) *Result
}
