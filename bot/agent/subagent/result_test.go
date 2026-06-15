package subagent

import (
	"strings"
	"testing"
)

func TestBuildResult(t *testing.T) {
	r := buildResult("hello world", runMeta{totalTokens: 100, toolUseCount: 3, durationMs: 500})
	if r.Status != StatusCompleted {
		t.Error("should be StatusCompleted")
	}
	if r.Content != "hello world" {
		t.Errorf("Content = %q", r.Content)
	}
	if r.classification != classPass {
		t.Errorf("classification = %v, want classPass", r.classification)
	}
}

func TestBuildResult_ClassifyWarn(t *testing.T) {
	r := buildResult("I ran rm -rf /tmp/old", runMeta{})
	if r.classification != classWarn {
		t.Errorf("classification = %v, want classWarn", r.classification)
	}
}

func TestBuildPartialResult(t *testing.T) {
	r := buildPartialResult("partial data", runMeta{})
	if r.Status != StatusPartial || r.Content != "partial data" {
		t.Error("wrong partial result")
	}
	if r.classification != classUnavailable {
		t.Errorf("partial = %v, want classUnavailable", r.classification)
	}
}

func TestBuildPartialResult_NoFalseWarn(t *testing.T) {
	r := buildPartialResult("ran rm -rf /tmp during cleanup", runMeta{})
	if r.classification != classUnavailable {
		t.Error("partial result should skip classification")
	}
}

func TestBuildFailedResult(t *testing.T) {
	r := buildFailedResult("connection refused", runMeta{})
	if r.Status != StatusFailed || r.Content != "connection refused" {
		t.Error("wrong failed result")
	}
}

func TestFormatResult_Normal(t *testing.T) {
	r := &Result{Content: "task done", classification: classPass}
	if s := FormatResult(r); s != "task done" {
		t.Errorf("FormatResult = %q, want %q", s, "task done")
	}
}

func TestFormatResult_Warn(t *testing.T) {
	r := &Result{Content: "dangerous output", classification: classWarn}
	s := FormatResult(r)
	if !strings.HasPrefix(s, "SECURITY WARNING:") {
		t.Error("classWarn should prefix with SECURITY WARNING")
	}
	if !strings.Contains(s, "dangerous output") {
		t.Error("should contain original content")
	}
}

func TestClassifyHandoff_Pass(t *testing.T) {
	for _, out := range []string{
		"Result: successfully added logging",
		"I read through the codebase and found the bug.",
	} {
		if got := classifyHandoff(out, runMeta{}); got != classPass {
			t.Errorf("classifyHandoff(%q) = %v, want classPass", out, got)
		}
	}
}

func TestClassifyHandoff_DangerousCommands(t *testing.T) {
	for _, c := range []string{
		"I ran rm -rf /tmp/build",
		"git push --force origin",
		"chmod 777 all",
		"> /dev/sda",
	} {
		if got := classifyHandoff(c, runMeta{}); got != classWarn {
			t.Errorf("classifyHandoff(%q) = %v, want classWarn", c, got)
		}
	}
}

func TestClassifyHandoff_SensitiveFiles(t *testing.T) {
	for _, c := range []string{
		"Modified .env file",
		"Wrote credentials file",
		"Read id_rsa",
	} {
		if got := classifyHandoff(c, runMeta{}); got != classWarn {
			t.Errorf("classifyHandoff(%q) = %v, want classWarn", c, got)
		}
	}
}

func TestClassifyHandoff_CaseInsensitive(t *testing.T) {
	if got := classifyHandoff("Ran RM -RF /tmp", runMeta{}); got != classWarn {
		t.Error("should be case insensitive")
	}
}

func TestClassifyHandoff_SensitiveOps(t *testing.T) {
	// Harmless text but subagent actually touched sensitive files.
	if got := classifyHandoff("I cleaned up the project directory", runMeta{sensitiveOps: 1}); got != classWarn {
		t.Errorf("classifyHandoff with sensitiveOps>0 = %v, want classWarn", got)
	}
}

func TestClassifyHandoff_SensitiveOpsZero(t *testing.T) {
	// Dangerous text still caught even without sensitiveOps.
	if got := classifyHandoff("I ran rm -rf /tmp", runMeta{}); got != classWarn {
		t.Errorf("classifyHandoff with dangerous text = %v, want classWarn", got)
	}
}
