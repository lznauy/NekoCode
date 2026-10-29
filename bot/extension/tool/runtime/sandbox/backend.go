// Package sandbox provides command-execution sandbox types. The concrete OS
// selection lives in the impl subpackage and is exposed through package
// functions in default.go.
package sandbox

import "nekocode/bot/extension/tool/runtime/sandbox/impl"

// Profile describes the sandbox environment for a command.
//
// A zero-value Profile (with only Workspace set) requests the strictest
// write-capable profile. The native backend isolates networking and /tmp and
// limits the filesystem view; reduced-isolation fallbacks may only enforce
// which paths are writable.
//
// Fields opened by capability grants incrementally relax isolation without
// leaving the sandbox:
//
//   - Network: authorize network use (the native backend shares the host
//     network namespace; a reduced fallback may already lack isolation).
//   - OutboundDomains: optional exact HTTP/HTTPS domain ceiling, enforced by
//     the Linux native backend; empty requires isolated offline execution.
//     Unsupported backends reject it rather than falling back.
//   - WritePaths: extra host directories bind-mounted as read-write.
//
// The sandbox package does NOT enforce a whitelist on WritePaths — the
// calling layer is responsible for authorization. Paths may use ~/ prefixes
// (resolved to the user's home) and are cleaned + deduplicated internally.
//
// This is a type alias to impl.Profile (defined in the impl subpackage) so
// the public sandbox package exposes the same struct callers use, while the
// canonical definition lives with the implementations.
type Profile = impl.Profile

type SandboxMode = impl.SandboxMode

const (
	ModeWorkspaceWrite = impl.ModeWorkspaceWrite
	ModeReadOnly       = impl.ModeReadOnly
)

// Process is a started sandbox or host process.
type Process = impl.Process

// UnavailableError is returned when no sandbox backend could be used.
// Callers should treat it as a signal to request host-execution permission.
//
// This is a type alias to impl.UnavailableError so that the public sandbox
// package exposes the same error type callers match on, while the canonical
// definition lives with the implementations.
type UnavailableError = impl.UnavailableError

// NetworkIsolationError reports a required egress boundary that cannot be
// enforced. It must not be treated as permission to execute on the host.
type NetworkIsolationError = impl.NetworkIsolationError
