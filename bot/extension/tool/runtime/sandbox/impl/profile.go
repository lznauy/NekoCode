package impl

type SandboxMode string

const (
	// ModeWorkspaceWrite is the default local-work mode: the workspace is
	// writable and network intent is controlled separately by Profile.Network.
	// Exact read visibility and isolation depend on the selected backend.
	ModeWorkspaceWrite SandboxMode = "workspace-write"

	// ModeReadOnly binds the workspace read-only. Explicit WritePaths, if any,
	// remain writable because callers must authorize them separately.
	ModeReadOnly SandboxMode = "read-only"
)

// Profile describes the sandbox environment for a command. The public
// sandbox.Profile type is a type alias pointing at this struct.
//
// A zero-value Profile (with only Workspace set) requests the strictest
// write-capable profile. The native backend provides filesystem, /tmp, and
// network isolation; reduced fallbacks may only enforce write restrictions.
type Profile struct {
	// Mode controls the filesystem access level for the workspace.
	// Empty is treated as ModeWorkspaceWrite for backward compatibility.
	Mode SandboxMode

	// Workspace is the primary working directory, bind-mounted read-write
	// at its host-absolute path unless ModeReadOnly is selected. Required.
	Workspace string

	// Network records whether network use is authorized.
	//   false (default) = request network isolation where the backend supports it
	//   true            = native backend shares the host network namespace
	Network bool

	// Non-nil requires fail-closed domain isolation; empty denies all egress.
	OutboundDomains []string
	// Internal socket used only by the native supervisor.
	EgressSocket string

	// WritePaths are extra host directories bind-mounted as read-write
	// inside the sandbox, in addition to Workspace. Use cases: package
	// manager caches, authorized external project directories.
	// Paths may use ~/ prefixes; they are resolved to absolute paths.
	WritePaths []string

	// ReadPaths are extra host directories bind-mounted read-only inside
	// the sandbox, in addition to Workspace. Use case: additional workspace
	// roots the user authorized as read-only — without this the native
	// backend hides them entirely and commands fail with "cannot access".
	// Paths may use ~/ prefixes; they are resolved to absolute paths.
	ReadPaths []string

	// StagingRoot is an internal field used by the native backend to pass
	// the host-side mountpoint to the re-exec'd child process (via JSON).
	// Callers should not set it — it is assigned automatically.
	StagingRoot string
}
