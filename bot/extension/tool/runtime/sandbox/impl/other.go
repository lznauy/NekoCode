//go:build !linux

package impl

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"nekocode/bot/extension/tool/runtime/toolutil"

	tbsb "github.com/tirdyhouse/sandbox"
)

// Run (macOS/Windows): uses the github.com/tirdyhouse/sandbox backend
// (macOS sandbox-exec / Windows Low Integrity Level) for file-write protection.
//
// The returned error is an UnavailableError when the platform backend is not
// available; callers should treat it as a signal to request host-execution
// permission.
func Run(ctx context.Context, command string, profile Profile, timeout time.Duration) (string, error) {
	if profile.OutboundDomains != nil {
		return "", NetworkIsolationError{Reason: "domain isolation requires Linux native namespaces"}
	}
	if profile.Workspace == "" {
		return "", fmt.Errorf("sandbox workspace is required")
	}
	ws, err := filepath.Abs(profile.Workspace)
	if err != nil {
		return "", fmt.Errorf("resolve workspace: %w", err)
	}

	if !tbsb.Available() {
		return "", UnavailableError{Reason: tbsb.ReasonUnavailable()}
	}

	var writable []string
	if profile.Mode != ModeReadOnly {
		writable = append(writable, ws)
	}
	writePaths, err := resolveWritePaths(profile.WritePaths)
	if err != nil {
		return "", err
	}
	writable = append(writable, writePaths...)

	// runTbsbBash runs command under the tirdyhouse/sandbox backend (macOS
	// sandbox-exec / Windows Low Integrity Level).
	//
	// It uses the package's public Command API, which applies the platform's
	// built-in sandbox so the command can only write to WritableDirs (workspace
	// plus allowed cache paths); everything else is read-only.
	//
	// NOTE: the public Cmd does not expose the child process handle, so per-run
	// timeout enforcement is not applied on these platforms. Long-running
	// commands rely on the caller's context cancellation. Native timeout/kill
	// is available on Linux via the namespace and Landlock backends.
	cmd := tbsb.Command("bash", "-c", command)
	cmd.Dir = ws
	cmd.Policy.WritableDirs = writable

	out, err := cmd.CombinedOutput()
	cleaned := toolutil.StripAnsi(string(out))
	cleaned = truncateCapturedOutput(cleaned)
	if err != nil {
		return "", fmt.Errorf("command failed: %v\nOutput: %s", err, cleaned)
	}
	return cleaned, nil
}

func Start(ctx context.Context, command string, profile Profile) (*Process, error) {
	if profile.OutboundDomains != nil {
		return nil, NetworkIsolationError{Reason: "domain isolation requires Linux native namespaces"}
	}
	return nil, UnavailableError{Reason: "async sandbox start is unavailable on this platform"}
}

// IsAvailable reports whether the current system has a usable sandbox backend.
func IsAvailable() bool {
	return tbsb.Available()
}
