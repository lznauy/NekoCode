package hooks

import (
	"fmt"
	"strconv"
	"strings"
)

func QuotaHint() InjectHook {
	return func(s *State) *Hint {
		if !s.QuotaHard {
			return nil
		}
		content := fmt.Sprintf("Hard quota: %d read(s), %d grep(s). Use the information you already have. If you must read more, apply for a quota extension in your response.",
			s.QuotaReadsLeft, s.QuotaReadsLeft)
		if s.QuotaReadsLeft <= 1 {
			return &Hint{Type: "quota", Severity: "critical", Content: content + " This is your last chance — summarize and act NOW."}
		}
		return &Hint{Type: "quota", Severity: "warning", Content: content}
	}
}

func VerificationHint() InjectHook {
	return func(s *State) *Hint {
		if s.NeedsVerification && !s.VerifyInjected {
			return &Hint{
				Type: "verification", Severity: "warning",
				Content: "You modified files. Before reporting done: 1) finish all pending tasks, 2) build + test, 3) verify. Do NOT stop here.",
			}
		}
		return nil
	}
}

func UnfinishedWorkHint() InjectHook {
	return func(s *State) *Hint {
		if s.NeedsVerification && s.VerifyInjected && s.ActionIsChat && !s.AllTasksDone {
			return &Hint{
				Type: "verification", Severity: "critical",
				Content: "You still have unfinished work. Complete ALL tasks, then build + test + verify. Do NOT summarize — ACT.",
			}
		}
		return nil
	}
}

func ModifiedFilesHint() InjectHook {
	return func(s *State) *Hint {
		if s.FilesModified && !s.NeedsVerification && !s.VerifyInjected {
			return &Hint{
				Type: "verification", Severity: "info",
				Content: "You modified files. Build + test to confirm, then report.",
			}
		}
		return nil
	}
}

func ExplorationExhaustedHint() InjectHook {
	return func(s *State) *Hint {
		if s.ExplorationScore <= 0 {
			return &Hint{
				Type: "exploration", Severity: "critical",
				Content: "You have explored enough. Do NOT call read/grep/glob/list anymore — further searching will only waste turns. Based on what you already know, either:\n1. Edit/write the files that need changes, or\n2. Report what you've found and what remains unclear.\n\nYour task: " + s.StepInput,
			}
		}
		return nil
	}
}

func ExplorationLowHint() InjectHook {
	return func(s *State) *Hint {
		if s.FilesModified {
			return nil
		}
		if s.ExplorationScore < 60 && s.ExplorationScore > 0 {
			return &Hint{
				Type: "exploration", Severity: "warning",
				Content: "You have not modified any files. The user asked you to MAKE CHANGES. Stop analyzing, start editing NOW.",
			}
		}
		return nil
	}
}

func ExploreCascadeHint() InjectHook {
	return func(s *State) *Hint {
		if s.ExploreCascade > 0 {
			return &Hint{
				Type: "explore_cascade", Severity: "warning",
				Content: "You've spawned " + strconv.Itoa(s.ExploreCascade) + " explore subagent(s) without modifying any files. If you need file content, use Read directly. If you've gathered enough information, synthesize your findings and act.\n\nYour task: " + s.StepInput,
			}
		}
		return nil
	}
}

func GarbledToolCallHint() InjectHook {
	return func(s *State) *Hint {
		if s.GarbledToolCall {
			return &Hint{
				Type: "dsml_leak", Severity: "critical",
				Content: "上轮未通过 function calling 调用工具——请直接用 function calling 重试。",
			}
		}
		return nil
	}
}

func RepeatedToolCallHint() InjectHook {
	return func(s *State) *Hint {
		if s.RepeatedCallCount < 2 {
			return nil
		}
		name := s.RepeatedCallName
		if idx := strings.IndexByte(name, '|'); idx >= 0 {
			name = name[:idx]
		}
		return &Hint{
			Type: "repeated_tool_call", Severity: "warning",
			Content: fmt.Sprintf(
				"You've called %q with the same parameters %d consecutive times — the result will not change. Stop re-running the same query and move forward with the information you already have.",
				name, s.RepeatedCallCount,
			),
		}
	}
}

func GarbledCircuitBreaker() StopHook {
	return func(s *State) (StopReason, bool) {
		if s.GarbledCount >= 3 {
			return StopFormatError, true
		}
		return 0, false
	}
}
