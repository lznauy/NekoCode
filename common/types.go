// types.go — shared types used by both bot and tui.
package common

// DangerLevel classifies tool risk for confirmation and planning.
type DangerLevel int

const (
	LevelSafe        DangerLevel = iota // read-only, auto-approve
	LevelWrite                          // file modification, confirm
	LevelDestructive                    // deletion, critical changes, confirm
	LevelForbidden                      // never allow
)

func (d DangerLevel) String() string {
	switch d {
	case LevelSafe:
		return "safe"
	case LevelWrite:
		return "modify"
	case LevelDestructive:
		return "danger"
	case LevelForbidden:
		return "blocked"
	default:
		return "unknown"
	}
}

// TodoItem represents a single task in the todo list.
type TodoItem struct {
	Content string `json:"content"`
	Status  string `json:"status"` // "pending", "in_progress", "completed"
}

// TodoFunc is called whenever the todo list is updated.
type TodoFunc func(items []TodoItem)
