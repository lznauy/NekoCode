package hooks

import (
	"strings"
	"testing"
)

func TestEmptyRegistry(t *testing.T) {
	r := NewRegistry()
	if len(r.EvaluateInject(&State{})) != 0 {
		t.Error("expected empty inject")
	}
	if _, ok := r.EvaluateStop(&State{}); ok {
		t.Error("expected no stop")
	}
	if FormatHints(nil) != "" || FormatHints([]Hint{}) != "" {
		t.Error("expected empty for nil/empty")
	}
}

func TestFormatHints(t *testing.T) {
	result := FormatHints([]Hint{{Type: "quota", Severity: "warning", Content: "3 reads left"}})
	if !strings.Contains(result, "<hints>") || !strings.Contains(result, `type="quota"`) {
		t.Errorf("bad format: %s", result)
	}
}

func TestQuotaHook(t *testing.T) {
	h := QuotaHint()
	if hint := h(&State{QuotaHard: false}); hint != nil {
		t.Error("green silent")
	}
	if hint := h(&State{QuotaHard: true, QuotaReadsLeft: 3}); hint == nil {
		t.Error("yellow fire")
	}
	if hint := h(&State{QuotaHard: true, QuotaReadsLeft: 1}); hint == nil || hint.Severity != "critical" {
		t.Error("red critical")
	}
}

func TestVerificationHook(t *testing.T) {
	h := VerificationHint()
	if hint := h(&State{}); hint != nil {
		t.Error("silent")
	}
	if hint := h(&State{NeedsVerification: true}); hint == nil {
		t.Error("should fire")
	}
	if hint := h(&State{NeedsVerification: true, VerifyInjected: true}); hint != nil {
		t.Error("already done")
	}
}

func TestUnfinishedWorkHook(t *testing.T) {
	h := UnfinishedWorkHint()
	if hint := h(&State{NeedsVerification: true, VerifyInjected: true, ActionIsChat: true}); hint == nil {
		t.Error("should fire")
	}
	if hint := h(&State{NeedsVerification: true, VerifyInjected: true, ActionIsChat: true, AllTasksDone: true}); hint != nil {
		t.Error("tasks done, silent")
	}
}

func TestGarbledCircuitBreaker(t *testing.T) {
	h := GarbledCircuitBreaker()
	if _, ok := h(&State{GarbledCount: 2}); ok {
		t.Error("should not stop at 2")
	}
	if reason, ok := h(&State{GarbledCount: 3}); !ok || reason != StopFormatError {
		t.Error("should stop at 3")
	}
}
