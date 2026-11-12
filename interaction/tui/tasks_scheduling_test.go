package tui

import (
	"strings"
	"testing"
)

func TestSchedulingUnknownValuesRemainVisible(t *testing.T) {
	for _, format := range []func(string) string{taskQueueReason, taskSchedulingNote} {
		if format("") != "" {
			t.Fatal("empty reason should stay empty")
		}
		if !strings.Contains(format("future_reason"), "future_reason") {
			t.Fatal("unknown reason hidden")
		}
	}
}
