package toolutil

import (
	"context"
	"fmt"
	"hash/fnv"
	nethttp "net/http"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"nekocode/bot/extension/tool/runtime/workspace"
	utilhttp "nekocode/util/http"
)

func StripAnsi(s string) string {
	return ansi.Strip(s)
}

func ValidatePath(path string) (string, error) {
	return workspace.Resolve(path)
}

func ValidatePathReadable(path string) (string, error) {
	return ValidatePathReadableContext(context.Background(), path)
}

func ValidatePathReadableContext(ctx context.Context, path string) (string, error) {
	manager := workspaceManager(ctx)
	safePath, _, allowed, err := manager.CheckRead(path)
	if err != nil {
		return "", err
	}
	if allowed {
		return safePath, nil
	}
	return "", fmt.Errorf("path %s is outside readable workspaces (roots: %v)", safePath, manager.Snapshot())
}

func ValidatePathWritable(path string) (string, error) {
	return ValidatePathWritableContext(context.Background(), path)
}

func ValidatePathWritableContext(ctx context.Context, path string) (string, error) {
	return validateWritable(ctx, path, false)
}

func ValidateDirectoryWritableContext(ctx context.Context, path string) (string, error) {
	return validateWritable(ctx, path, true)
}

func validateWritable(ctx context.Context, path string, directory bool) (string, error) {
	manager := workspaceManager(ctx)
	check := manager.CheckWrite
	if directory {
		check = manager.CheckWriteDirectory
	}
	safePath, _, allowed, err := check(path)
	if err != nil {
		return "", err
	}
	if allowed {
		return safePath, nil
	}
	return "", fmt.Errorf("path %s is outside writable workspaces (roots: %v)", safePath, manager.Snapshot())
}

func workspaceManager(ctx context.Context) *workspace.Manager {
	if manager, ok := workspace.FromContext(ctx); ok {
		return manager
	}
	return workspace.New("", nil)
}

func NormalizeText(text string) string {
	text = StripAnsi(text)
	return NormalizeToLF(text)
}

func ReadSafeFile(path string) ([]byte, error) {
	return ReadSafeFileContext(context.Background(), path)
}

func ReadSafeFileContext(ctx context.Context, path string) ([]byte, error) {
	safePath, err := ValidatePathReadableContext(ctx, path)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(safePath)
}

func NewToolHTTPClient(timeout time.Duration) *nethttp.Client {
	return &nethttp.Client{
		Transport: utilhttp.SharedTransport,
		Timeout:   timeout,
	}
}

// NormalizeToLF converts all line endings to LF.
func NormalizeToLF(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return text
}

// DetectLineEnding returns the first line ending found ("\r\n" or "\n").
// Defaults to "\n" if no line endings are present.
func DetectLineEnding(text string) string {
	if strings.Contains(text, "\r\n") {
		return "\r\n"
	}
	return "\n"
}

// RestoreLineEndings converts LF back to the original line ending style.
func RestoreLineEndings(text, lineEnding string) string {
	if lineEnding == "\n" {
		return text
	}
	// First normalize to LF, then convert to target.
	text = NormalizeToLF(text)
	return strings.ReplaceAll(text, "\n", lineEnding)
}

// ComputeFileHash returns an 8-char uppercase hex tag from normalized text.
// The hash is stable across CRLF/LF differences and trailing whitespace.
// Uses full 32-bit FNV-1a (4 billion possible values) to minimize collision risk.
func ComputeFileHash(text string) string {
	norm := normalizeForHash(text)
	h := fnv.New32a()
	h.Write([]byte(norm))
	return fmt.Sprintf("%08X", h.Sum32())
}

// normalizeForHash canonicalizes text for hashing: CRLF→LF, strip trailing
// whitespace per line, strip final trailing newline.
func normalizeForHash(text string) string {
	text = NormalizeToLF(text)
	lines := strings.Split(text, "\n")
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(strings.TrimRight(line, " \t"))
	}
	// Strip trailing newline.
	return strings.TrimRight(b.String(), "\n")
}
