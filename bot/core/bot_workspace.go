package core

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"nekocode/protocol"
)

const workspaceStatusTimeout = 750 * time.Millisecond

// WorkspaceChanges returns aggregate tracked line changes relative to HEAD
// and the number of untracked files. It never includes paths or file content.
func (b *Bot) WorkspaceChanges() protocol.WorkspaceChanges {
	ctx, cancel := context.WithTimeout(context.Background(), workspaceStatusTimeout)
	defer cancel()

	untracked, err := gitOutput(ctx, b.cwd, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return protocol.WorkspaceChanges{}
	}
	diffArgs := []string{"diff", "--numstat", "--no-renames", "--no-ext-diff", "--no-textconv", "-z"}
	stats, err := gitOutput(ctx, b.cwd, append(diffArgs, "HEAD", "--")...)
	if err != nil {
		staged, stagedErr := gitOutput(ctx, b.cwd, append(diffArgs, "--cached", "--")...)
		working, workingErr := gitOutput(ctx, b.cwd, append(diffArgs, "--")...)
		if stagedErr != nil || workingErr != nil {
			return protocol.WorkspaceChanges{}
		}
		stats = append(staged, working...)
	}
	added, deleted := parseNumstat(stats)
	return protocol.WorkspaceChanges{
		Added: added, Deleted: deleted,
		Untracked: countNULTerms(untracked), Available: true,
	}
}

func gitOutput(ctx context.Context, cwd string, args ...string) ([]byte, error) {
	base := []string{"-C", cwd, "-c", "protocol.allow=never", "-c", "core.hooksPath=" + os.DevNull, "-c", "core.fsmonitor=false", "-c", "core.sparseCheckout=false", "-c", "submodule.recurse=false", "-c", "core.untrackedCache=false"}
	env := make([]string, 0, len(os.Environ()))
	// Ignore inherited repository selectors and command-scoped configuration.
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(strings.ToUpper(name), "GIT_") {
			env = append(env, entry)
		}
	}
	env = append(env, "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_NO_LAZY_FETCH=1", "GIT_NO_REPLACE_OBJECTS=1")
	run := func(arguments ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "git", append(append([]string{}, base...), arguments...)...)
		cmd.Env = env
		return cmd.Output()
	}
	// Diff converts working-tree contents through clean/process filters even
	// with --no-ext-diff and --no-textconv. Disable every configured driver.
	config, err := run("config", "--null", "--get-regexp", `^filter\..*\.(clean|smudge|process|required)$`)
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			return nil, err
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
	return run(args...)
}

func parseNumstat(data []byte) (added, deleted int) {
	for _, record := range bytes.Split(data, []byte{0}) {
		first := bytes.IndexByte(record, '\t')
		if first < 0 {
			continue
		}
		secondOffset := bytes.IndexByte(record[first+1:], '\t')
		if secondOffset < 0 {
			continue
		}
		second := first + 1 + secondOffset
		added += parseStat(record[:first])
		deleted += parseStat(record[first+1 : second])
	}
	return added, deleted
}

func parseStat(value []byte) int {
	n, _ := strconv.Atoi(string(value)) // Binary files use "-" and contribute zero lines.
	return max(n, 0)
}

func countNULTerms(data []byte) int {
	return bytes.Count(data, []byte{0})
}
