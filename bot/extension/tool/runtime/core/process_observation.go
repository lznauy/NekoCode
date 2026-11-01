package core

// ProcessObservation is a sampled process fact, independent of tool-call errors.
// Empty State means the tool supplied no process observation. ExitCodeKnown is
// true only for an observed normal exit; zero alone never implies success.
// OutputComplete means this sample contains all captured stdout/stderr before
// text normalization, not that the command or a business task succeeded.
type ProcessObservation struct {
	State          string
	ExitCode       int
	ExitCodeKnown  bool
	OutputComplete bool
}
