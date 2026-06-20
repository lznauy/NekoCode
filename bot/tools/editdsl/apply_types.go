package editdsl

// ApplyResult holds the outcome of applying edits.
type ApplyResult struct {
	Text             string
	FirstChangedLine int
	Warnings         []string
	ResolvedHunks    []Hunk
	OldToNew         map[int]int
}

// BlockSpan represents the resolved line range of a code block.
type BlockSpan struct {
	Start int
	End   int
}

// BlockResolver resolves a line number to the enclosing code block's span.
type BlockResolver func(path string, line int) (*BlockSpan, error)

// autoprefixSentinel is used by parsePayload to mark bare body rows that were
// auto-prefixed. ApplyEdits strips it and emits a warning.
const autoprefixSentinel = "\x00autoprefix:"
