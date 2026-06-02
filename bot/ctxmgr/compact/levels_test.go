package compact

import "testing"

func TestLevel_String(t *testing.T) {
	tests := []struct {
		l    Level
		want string
	}{
		{LevelNormal, "normal"},
		{LevelWarning, "warning"},
		{LevelMicroCompact, "micro_compact"},
		{LevelCompact, "compact"},
		{LevelBlocking, "blocking"},
		{Level(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.l.String(); got != tt.want {
			t.Errorf("Level(%d) = %q, want %q", tt.l, got, tt.want)
		}
	}
}

func TestClassifyLevel(t *testing.T) {
	cfg := DefaultConfig
	tests := []struct {
		remaining int
		want      Level
	}{
		{0, LevelBlocking},
		{cfg.BlockingBuffer, LevelBlocking},
		{cfg.BlockingBuffer + 1, LevelCompact},
		{cfg.CompactBuffer, LevelCompact},
		{cfg.CompactBuffer + 1, LevelMicroCompact},
		{cfg.MicroCompactBuffer, LevelMicroCompact},
		{cfg.MicroCompactBuffer + 1, LevelWarning},
		{cfg.WarningBuffer, LevelWarning},
		{cfg.WarningBuffer + 1, LevelNormal},
	}
	for _, tt := range tests {
		if got := classifyLevel(tt.remaining, cfg); got != tt.want {
			t.Errorf("classifyLevel(%d) = %s, want %s", tt.remaining, got, tt.want)
		}
	}
}
