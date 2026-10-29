package worktree

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Management operations must not execute repository hooks, filters or helpers.
// User shell commands remain governed by the normal execution permission layer.
func git(ctx context.Context, cwd string, input []byte, extraEnv []string, args ...string) ([]byte, error) {
	env := make([]string, 0, len(os.Environ()))
	for _, e := range os.Environ() {
		name, _, _ := strings.Cut(e, "=")
		if !strings.HasPrefix(strings.ToUpper(name), "GIT_") {
			env = append(env, e)
		}
	}
	env = append(env, "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_NO_LAZY_FETCH=1", "GIT_NO_REPLACE_OBJECTS=1")
	base := []string{"-c", "protocol.allow=never", "-c", "core.hooksPath=" + os.DevNull, "-c", "core.fsmonitor=false", "-c", "core.sparseCheckout=false", "-c", "submodule.recurse=false", "-c", "core.untrackedCache=false"}
	run := func(a []string, stdin []byte, extras []string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "git", append(append([]string{"-C", cwd}, base...), a...)...)
		cmd.Env = append(append([]string{}, env...), extras...)
		cmd.Stdin = bytes.NewReader(stdin)
		var out, stderr bytes.Buffer
		cmd.Stdout = &boundedBuffer{buffer: &out, limit: 64 << 20}
		cmd.Stderr = &boundedBuffer{buffer: &stderr, limit: 64 << 10}
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("git %s: %w: %s", a[0], err, strings.TrimSpace(stderr.String()))
		}
		return out.Bytes(), nil
	}
	// Config querying does not invoke filters. Exit 1 means no matching keys.
	config, configErr := run([]string{"config", "--null", "--get-regexp", `^filter\..*\.(clean|smudge|process|required)$`}, nil, nil)
	if configErr != nil {
		var exit *exec.ExitError
		if !errors.As(configErr, &exit) || exit.ExitCode() != 1 {
			return nil, configErr
		}
	}
	for _, record := range bytes.Split(config, []byte{0}) {
		key, _, ok := strings.Cut(string(record), "\n")
		if !ok {
			continue
		}
		value := ""
		if strings.HasSuffix(key, ".required") {
			value = "false"
		}
		base = append(base, "-c", key+"="+value)
	}
	return run(args, input, extraEnv)
}

// Bound binary patches and diagnostics instead of buffering arbitrary repositories.
type boundedBuffer struct {
	buffer *bytes.Buffer
	limit  int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.buffer.Len() {
		return 0, fmt.Errorf("git output exceeds size limit: %w", io.ErrShortBuffer)
	}
	return b.buffer.Write(p)
}
