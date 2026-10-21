package core

import (
	"context"
	"fmt"
	"nekocode/protocol"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"
)

// MaxOutputBytes bounds every tool's returned text, including errors. Bytes
// also provide a conservative token bound for UTF-8 text without a tokenizer.
const MaxOutputBytes = 16 * 1024

const outputTruncated = "\n[Tool output truncated; narrow the query or read a smaller range.]\n"

// LimitOutput preserves both ends without splitting UTF-8 characters. The
// marker is included in the limit. Tiny budgets retain only a marker prefix.
func LimitOutput(content string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(content) <= limit {
		return content
	}
	if limit <= len(outputTruncated) {
		return outputTruncated[:limit]
	}
	available := limit - len(outputTruncated)
	head := (available + 1) / 2
	tail := len(content) - available/2
	for head > 0 && !utf8.RuneStart(content[head]) {
		head--
	}
	for tail < len(content) && !utf8.RuneStart(content[tail]) {
		tail++
	}
	return content[:head] + outputTruncated + content[tail:]
}

// OutputStore owns one actor's result artifacts. A configured directory lives
// inside the session; an empty directory uses a private temporary fallback.
type OutputStore struct {
	mu     sync.Mutex
	dir    string
	parent *OutputStore
	child  string
}

func NewOutputStore(dir string) *OutputStore { return &OutputStore{dir: dir} }

// Child gives a delegated actor its own namespace under the parent's store.
func (s *OutputStore) Child(name string) *OutputStore {
	return &OutputStore{parent: s, child: name}
}

func (s *OutputStore) Directory() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dir == "" && s.parent != nil {
		if s.child == "" || filepath.Base(s.child) != s.child || strings.ContainsAny(s.child, `/\\`) || s.child == "." || s.child == ".." {
			return "", fmt.Errorf("invalid output store namespace")
		}
		base, err := s.parent.Directory()
		if err != nil {
			return "", err
		}
		s.dir = filepath.Join(base, s.child)
	}
	if s.dir == "" {
		dir, err := os.MkdirTemp("", "nekocode-tool-results-")
		if err != nil {
			return "", err
		}
		s.dir = dir
	}
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return "", err
	}
	return s.dir, nil
}

// PreserveOutput stores the exact original in a private file.
func (s *OutputStore) PreserveOutput(content string) (string, error) {
	file, err := s.createOutput()
	if err != nil {
		return "", err
	}
	_, writeErr := file.WriteString(content)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil {
		return "", writeErr
	}
	if syncErr != nil {
		return "", syncErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	return file.Name(), nil
}

// ToolOutput carries returned text and an independent artifact reference.
// Text never contains a framework-generated artifact header.
type ToolOutput struct {
	Text     string
	Artifact protocol.ToolOutputArtifact
}

// Render formats the model/UI view without encoding metadata into the contract.
func (o ToolOutput) Render() string {
	if o.Artifact.Path == "" {
		return o.Text
	}
	return OutputReferenceHeader(o.Artifact.Path, !o.Artifact.Incomplete) + o.Text
}

// BoundOutput preserves an existing artifact when narrowing its preview.
func (s *OutputStore) BoundOutput(output ToolOutput, limit int) (ToolOutput, error) {
	if output.Artifact.Path != "" {
		if err := s.CheckArtifact(output.Artifact); err != nil {
			return ToolOutput{}, err
		}
	}
	if len(output.Render()) <= limit {
		return output, nil
	}
	if output.Artifact.Path == "" {
		path, err := s.PreserveOutput(output.Text)
		if err != nil {
			return ToolOutput{}, err
		}
		output.Artifact = protocol.ToolOutputArtifact{Path: path}
	}
	header := OutputReferenceHeader(output.Artifact.Path, !output.Artifact.Incomplete)
	if len(header) > limit {
		return ToolOutput{}, fmt.Errorf("output budget %d cannot hold a complete result reference", limit)
	}
	output.Text = LimitOutput(output.Text, limit-len(header))
	return output, nil
}

// CheckArtifact enforces actor ownership before granting or restoring a reference.
func (s *OutputStore) CheckArtifact(artifact protocol.ToolOutputArtifact) error {
	dir, err := s.Directory()
	if err != nil {
		return err
	}
	info, err := os.Lstat(artifact.Path)
	if err != nil || filepath.Dir(artifact.Path) != dir || !info.Mode().IsRegular() {
		return fmt.Errorf("tool output artifact does not belong to this actor or is unavailable")
	}
	return nil
}

// DeferredOutput is a complete status even when no content budget remains.
const DeferredOutput = "[Tool result deferred: insufficient context budget. Run /compact or start a new session before continuing.]"
const DeferredOutputSource = "tool_output_deferred"

func OutputReferenceHeader(path string, complete bool) string {
	label := "Full result"
	if !complete {
		label = "Saved prefix"
	}
	return fmt.Sprintf("[Tool output truncated. %s: %s. Read in smaller ranges.]\n", label, path)
}

func (s *OutputStore) createOutput() (*os.File, error) {
	dir, err := s.Directory()
	if err != nil {
		return nil, err
	}
	return os.CreateTemp(dir, "result-*.txt")
}

type outputStoreKey struct{}

func WithOutputStore(ctx context.Context, store *OutputStore) context.Context {
	return context.WithValue(ctx, outputStoreKey{}, store)
}

func OutputStoreFromContext(ctx context.Context) *OutputStore {
	store, _ := ctx.Value(outputStoreKey{}).(*OutputStore)
	return store
}
