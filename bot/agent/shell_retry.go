package agent

import (
	"regexp"
	"strings"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/policy"
)

// Keep just the last shell invocation in this run. Intervening file edits
// are expected; another shell command starts a different comparison.
type shellFailure struct {
	args, exception, frame string
}

var pythonFrame = regexp.MustCompile(`^  File "(?:[^"]*/)?([A-Za-z0-9_.-]{1,80}\.py)", line ([0-9]{1,8})(?:, in .*)?$`)
var pythonException = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]{0,63}(?:Error|Exception))(?::|$)`)

func retryFailure(result core.ToolCallResult) shellFailure {
	receipts := result.Evidence.Executions
	if result.Error != "" || len(receipts) != 1 || result.Evidence.PreservationFailed {
		return shellFailure{}
	}
	r := receipts[0]
	if r.Tool != "shell" || r.ArgumentsHash == "" || r.Status != "error" || !r.Process.ExitCodeKnown || r.Process.ExitCode == 0 || !r.Process.OutputComplete {
		return shellFailure{}
	}
	_, trace, ok := strings.Cut(result.Output, "Traceback (most recent call last):\n")
	if !ok || strings.Contains(trace, "Traceback (most recent call last):") {
		return shellFailure{}
	}
	var frame string
	for line := range strings.SplitSeq(trace, "\n") {
		if m := pythonFrame.FindStringSubmatch(line); m != nil {
			frame = m[1] + ":" + m[2]
		} else if m := pythonException.FindStringSubmatch(line); m != nil && frame != "" {
			return shellFailure{r.ArgumentsHash, m[1], frame}
		}
	}
	return shellFailure{}
}

func (a *Agent) observeShellRetry(results []core.ToolCallResult) {
	for _, result := range results {
		if result.Name != "shell" {
			continue
		}
		prev := a.run.lastShellFailure
		a.run.lastShellFailure = shellFailure{}
		if len(results) != 1 {
			return // Parallel operations do not establish a serial retry.
		}
		cur := retryFailure(result)
		a.run.lastShellFailure = cur
		if cur.args != "" && prev.args == cur.args && prev.exception != cur.exception {
			a.injectHint(&policy.Hint{Type: "shell_retry", Content: "Consecutive executions of the same shell call reported different Python tracebacks: " +
				prev.exception + " at " + prev.frame + " → " + cur.exception + " at " + cur.frame + "."})
		}
	}
}
