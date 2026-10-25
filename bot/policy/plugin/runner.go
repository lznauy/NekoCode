package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"nekocode/bot/policy/contract"
	"nekocode/logger"
	"os"
	"os/exec"
	"sync"
	"time"
)

func runPluginCommandEvent(pluginRoot string, event Event, action hookAction) (output string, truncated bool, err error) {
	event = contract.NormalizeEvent(event)
	cmd := action.Command

	timeout := action.Timeout
	if timeout <= 0 {
		timeout = 5000
	}

	ctx, cancel := context.WithTimeout(eventContext(event), time.Duration(timeout)*time.Millisecond)
	defer cancel()

	c := exec.CommandContext(ctx, "sh", "-c", cmd)
	c.Dir = pluginRoot
	c.Env = append(os.Environ(), "PLUGIN_ROOT="+pluginRoot, "CLAUDE_PLUGIN_ROOT="+pluginRoot)
	event.PluginRoot = pluginRoot
	input, err := json.Marshal(event)
	if err != nil {
		return "", false, err
	}
	c.Stdin = bytes.NewReader(input)
	setupHookProcess(c)
	// A child retaining stdout must not keep Run blocked indefinitely.
	c.WaitDelay = 100 * time.Millisecond
	var out cappedOutput
	var diagnostic cappedOutput
	c.Stdout = &out
	c.Stderr = &diagnostic
	started := time.Now()
	runErr := c.Run()
	finishHookProcess(c)
	if ctx.Err() != nil {
		runErr = ctx.Err()
	}
	if runErr != nil && len(diagnostic.data) > 0 {
		runErr = fmt.Errorf("%w: %s", runErr, diagnostic.data)
	}
	logger.Log("hook_command session=%q run=%q actor=%q hook=%q event=%q evaluation_id=%d action_index=%d duration_ms=%d error=%q stderr=%q stderr_truncated=%t",
		event.SessionID, event.RunID, event.ActorID, event.HookID, event.Point, event.EvaluationID, event.ActionIndex, time.Since(started).Milliseconds(), commandError(runErr), string(diagnostic.data), diagnostic.truncated)
	raw := string(out.data)

	if out.truncated {
		raw = raw + "\n[... truncated at " + fmt.Sprint(maxPluginOutputBytes) + " bytes]"
		truncated = true
	}
	return raw, truncated, runErr
}

// Keep draining output after the cap so producers cannot block on a full pipe.
type cappedOutput struct {
	mu        sync.Mutex
	data      []byte
	truncated bool
}

func (b *cappedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	remaining := maxPluginOutputBytes - len(b.data)
	if len(p) > remaining {
		p = p[:remaining]
		b.truncated = true
	}
	b.data = append(b.data, p...)
	return n, nil
}

func eventContext(e Event) context.Context {
	if e.Context != nil {
		return e.Context
	}
	return context.Background()
}

func commandError(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
