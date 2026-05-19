package tools

import (
	"nekocode/common"
	"testing"
)

func TestDangerLevelString(t *testing.T) {
	tests := []struct {
		level common.DangerLevel
		want  string
	}{
		{common.LevelSafe, "safe"},
		{common.LevelWrite, "modify"},
		{common.LevelDestructive, "danger"},
		{common.LevelForbidden, "blocked"},
	}
	for _, tt := range tests {
		if got := tt.level.String(); got != tt.want {
			t.Errorf("%d.String() = %q, want %q", tt.level, got, tt.want)
		}
	}
}
