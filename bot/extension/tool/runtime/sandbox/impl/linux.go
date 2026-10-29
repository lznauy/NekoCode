//go:build linux

package impl

import (
	"context"
	"fmt"
	"time"
)

// Run (Linux): prefers the native namespace backend (pivot_root +
// network/pid isolation). If user namespaces are unavailable (disabled by
// env or rejected by the kernel/container), falls back to the Landlock
// backend (file-write protection only, no network isolation).
//
// The returned error is an UnavailableError when no backend could be used;
// callers should treat it as a signal to request host-execution permission.
func Run(ctx context.Context, command string, profile Profile, timeout time.Duration) (string, error) {
	// Validate the workspace up front so callers always get a clear
	// "workspace is required" error rather than a confusing backend-specific
	// failure (or an UnavailableError from a doomed landlock attempt).
	if profile.OutboundDomains != nil {
		if !isNativeAvailable() {
			return "", NetworkIsolationError{Reason: "Linux native namespaces required"}
		}
		out, err := runNativeBash(ctx, command, profile, timeout)
		if err != nil {
			if _, ok := err.(UnavailableError); ok {
				return "", NetworkIsolationError{Reason: err.Error()}
			}
		}
		return out, err
	}
	if profile.Workspace == "" {
		return "", fmt.Errorf("sandbox workspace is required")
	}

	// Fast path: skip the native attempt entirely when it is known to be
	// unavailable (NEKOCODE_DISABLE_NATIVE_SANDBOX set, or unshare probe
	// fails — common on CI/restricted kernels). Avoids spawning a doomed
	// child just to classify its failure.
	if !isNativeAvailable() {
		if landlockAvailable() {
			if out, err := runLandlockBash(ctx, command, profile, timeout); err == nil {
				return out, nil
			}
		}
		return "", UnavailableError{Reason: "native sandbox unavailable and landlock failed or unavailable"}
	}

	out, err := runNativeBash(ctx, command, profile, timeout)
	if err == nil {
		return out, nil
	}
	// Namespace creation can fail at runtime even when the probe passed
	// (e.g. mount denied mid-setup). Fall back to the Landlock backend.
	if unavail, ok := err.(UnavailableError); ok {
		if landlockAvailable() {
			if lbOut, lbErr := runLandlockBash(ctx, command, profile, timeout); lbErr == nil {
				return lbOut, nil
			}
		}
		return "", unavail
	}
	return out, err
}

// Start launches a long-running command in the best available sandbox.
func Start(ctx context.Context, command string, profile Profile) (*Process, error) {
	if profile.OutboundDomains != nil {
		if !isNativeAvailable() {
			return nil, NetworkIsolationError{Reason: "Linux native namespaces required"}
		}
		p, err := startNativeBash(ctx, command, profile)
		if err != nil {
			if _, ok := err.(UnavailableError); ok {
				return nil, NetworkIsolationError{Reason: err.Error()}
			}
		}
		return p, err
	}
	if profile.Workspace == "" {
		return nil, fmt.Errorf("sandbox workspace is required")
	}
	if !isNativeAvailable() {
		if landlockAvailable() {
			if p, err := startLandlockBash(ctx, command, profile); err == nil {
				return p, nil
			}
		}
		return nil, UnavailableError{Reason: "native sandbox unavailable and landlock failed or unavailable"}
	}
	p, err := startNativeBash(ctx, command, profile)
	if err == nil {
		return p, nil
	}
	if _, ok := err.(UnavailableError); ok && landlockAvailable() {
		if lbp, lbErr := startLandlockBash(ctx, command, profile); lbErr == nil {
			return lbp, nil
		}
	}
	return nil, err
}

// IsAvailable reports whether the current system has at least one usable
// sandbox backend.
func IsAvailable() bool {
	return isNativeAvailable() || landlockAvailable()
}
