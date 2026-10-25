package session

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const SubagentOutputBytes = 64 << 10

type SubagentOutputPage struct {
	Content                   string
	Offset, NextOffset, Total int64
	HasMore, Incomplete       bool
}

// SubagentToolOutput resolves an artifact from a committed tool-result message.
// The observer supplies a message sequence, never an arbitrary filesystem path.
func SubagentToolOutput(parentID, id string, sequence int, offset int64) (SubagentOutputPage, error) {
	if sequence < 1 || sequence == int(^uint(0)>>1) || offset < 0 {
		return SubagentOutputPage{}, fmt.Errorf("invalid tool output cursor")
	}
	history, err := SubagentHistory(parentID, id, sequence+1, 1)
	if err != nil {
		return SubagentOutputPage{}, err
	}
	if history.End != sequence || len(history.Messages) == 0 {
		return SubagentOutputPage{}, fmt.Errorf("tool result is not committed")
	}
	msg := history.Messages[len(history.Messages)-1]
	if msg.Role != "tool" || msg.ToolOutput.Path == "" {
		return SubagentOutputPage{}, fmt.Errorf("tool result has no stored output")
	}
	base, err := ToolOutputDir(parentID)
	if err != nil {
		return SubagentOutputPage{}, err
	}
	expected := filepath.Join(base, "subagents", id)
	if filepath.Dir(msg.ToolOutput.Path) != expected {
		return SubagentOutputPage{}, fmt.Errorf("tool output does not belong to this subagent")
	}
	baseReal, err := filepath.EvalSymlinks(base)
	if err != nil {
		return SubagentOutputPage{}, err
	}
	actualDir, err := filepath.EvalSymlinks(expected)
	if err != nil {
		return SubagentOutputPage{}, err
	}
	if actualDir != filepath.Join(baseReal, "subagents", id) {
		return SubagentOutputPage{}, fmt.Errorf("tool output directory escapes this subagent")
	}
	info, err := os.Lstat(msg.ToolOutput.Path)
	if err != nil {
		return SubagentOutputPage{}, err
	}
	if !info.Mode().IsRegular() {
		return SubagentOutputPage{}, fmt.Errorf("tool output is not a regular file")
	}
	f, err := os.Open(msg.ToolOutput.Path)
	if err != nil {
		return SubagentOutputPage{}, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return SubagentOutputPage{}, err
	}
	if !os.SameFile(info, opened) {
		return SubagentOutputPage{}, fmt.Errorf("tool output changed while opening")
	}
	if offset > opened.Size() {
		return SubagentOutputPage{}, fmt.Errorf("tool output cursor exceeds file size")
	}
	buffer := make([]byte, SubagentOutputBytes+utf8.UTFMax)
	n, err := f.ReadAt(buffer, offset)
	if err != nil && err != io.EOF {
		return SubagentOutputPage{}, err
	}
	end := min(n, SubagentOutputBytes)
	for back := 0; end > 0 && end < n && !utf8.RuneStart(buffer[end]) && back < utf8.UTFMax-1; back++ {
		end--
	}
	return SubagentOutputPage{Content: strings.ToValidUTF8(string(buffer[:end]), "�"), Offset: offset, NextOffset: offset + int64(end), Total: opened.Size(), HasMore: offset+int64(end) < opened.Size(), Incomplete: msg.ToolOutput.Incomplete}, nil
}
