package editdsl

// Patch represents a complete hashline patch.
type Patch struct {
	Files []FilePatch
}

// FilePatch represents edits to a single file.
type FilePatch struct {
	Path    string
	FileTag string
	Hunks   []Hunk
}

// HunkKind enumerates the supported edit operations.
type HunkKind int

const (
	HunkReplace HunkKind = iota
	HunkDelete
	HunkInsert
)

// CursorType specifies where an insert hunk lands relative to its anchor.
type CursorType int

const (
	CursorBefore CursorType = iota
	CursorAfter
	CursorHead
	CursorTail
)

// Hunk represents a single edit operation within a file.
type Hunk struct {
	Kind    HunkKind
	Start   int
	End     int
	Cursor  CursorType
	Block   bool
	Payload []string
}
