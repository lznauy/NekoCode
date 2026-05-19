package compact

// Level defines the urgency of compression.
type Level int

const (
	LevelNormal       Level = iota
	LevelWarning
	LevelMicroCompact
	LevelCompact
	LevelBlocking
)

func (l Level) String() string {
	switch l {
	case LevelNormal:
		return "normal"
	case LevelWarning:
		return "warning"
	case LevelMicroCompact:
		return "micro_compact"
	case LevelCompact:
		return "compact"
	case LevelBlocking:
		return "blocking"
	default:
		return "unknown"
	}
}

// Config holds auto-compaction thresholds.
type Config struct {
	WarningBuffer      int
	MicroCompactBuffer int
	CompactBuffer      int
	BlockingBuffer     int
}

// DefaultConfig is tuned for a 64K budget. Thresholds are scaled for larger budgets.
var DefaultConfig = Config{
	WarningBuffer:      44800,
	MicroCompactBuffer: 35200,
	CompactBuffer:      25600,
	BlockingBuffer:     6400,
}

func classifyLevel(remaining int, cfg Config) Level {
	if remaining <= cfg.BlockingBuffer {
		return LevelBlocking
	}
	if remaining <= cfg.CompactBuffer {
		return LevelCompact
	}
	if remaining <= cfg.MicroCompactBuffer {
		return LevelMicroCompact
	}
	if remaining <= cfg.WarningBuffer {
		return LevelWarning
	}
	return LevelNormal
}
