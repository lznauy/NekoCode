package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/toolutil"
	"nekocode/protocol"
)

// webResult applies delivery limits only after preserving the retrieved source.
// Artifact ownership, persistence and later reads use the runtime's actor store.
func webResult(ctx context.Context, source, method, content, prompt string, limit int, fallback bool) (core.ToolOutput, error) {
	if err := ctx.Err(); err != nil {
		return core.ToolOutput{}, err
	}
	content = strings.TrimSpace(toolutil.StripAnsi(content))
	header := "retrieval_method: " + method + "\nsource_completeness: unverified\n"
	if fallback {
		header += "[Fallback extraction: completeness is not independently verified.]\n"
	}
	header += "\n"
	preview := finalizeContentWithLimit(content, prompt, max(1, limit-len([]rune(header))))
	output := core.ToolOutput{Text: header + preview}
	// A short unfiltered result already lives in session history. Preserve the
	// full source whenever any layer would otherwise hide part of it.
	if preview == content && len(output.Text) <= core.MaxOutputBytes {
		return output, nil
	}
	store := core.OutputStoreFromContext(ctx)
	if store == nil {
		store = core.NewOutputStore("")
	}
	metadata, err := json.Marshal(struct {
		Source       string `json:"source_url"`
		Method       string `json:"retrieval_method"`
		RetrievedAt  string `json:"retrieved_at"`
		Completeness string `json:"source_completeness"`
		Fallback     bool   `json:"fallback"`
	}{safeSourceURL(source), method, time.Now().UTC().Format(time.RFC3339Nano), "unverified", fallback})
	if err != nil {
		return core.ToolOutput{}, err
	}
	snapshot := "# Retrieved web content\n" + string(metadata) + "\n\n" + content
	path, err := store.PreserveOutput(snapshot)
	if err != nil {
		return core.ToolOutput{}, fmt.Errorf("preserve complete retrieved web content: %w", err)
	}
	output.Artifact = protocol.ToolOutputArtifact{Path: path}
	const outlineHeader = "Saved-source sections (use read with these line numbers):\n"
	outline := webOutline(content, 4, min(1000, max(0, (limit-len([]rune(header)))/3)-len(outlineHeader)))
	if outline != "" {
		header += outlineHeader + outline + "\n"
	}
	output.Text = header + finalizeContentWithLimit(content, prompt, max(1, limit-len([]rune(header))))
	return output, nil
}

func safeSourceURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	u.User = nil
	u.RawQuery = redactQuery(u.RawQuery)
	u.Fragment = ""
	return u.String()
}

func webOutline(content string, firstLine, budget int) string {
	var rows []string
	var fence byte
	width := 0
	for i, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if fence != 0 {
			run := 0
			for run < len(trimmed) && trimmed[run] == fence {
				run++
			}
			if run >= width && strings.TrimSpace(trimmed[run:]) == "" {
				fence = 0
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fence = trimmed[0]
			width = 0
			for width < len(trimmed) && trimmed[width] == fence {
				width++
			}
			continue
		}
		if paragraphHeading(line) > 0 {
			row := fmt.Sprintf("%d: %s", firstLine+i, truncateWebContent(trimmed, 100))
			budget -= len([]rune(row)) + 1
			if budget < 0 {
				break
			}
			rows = append(rows, row)
			if len(rows) == 10 {
				break
			}
		}
	}
	return strings.Join(rows, "\n")
}
