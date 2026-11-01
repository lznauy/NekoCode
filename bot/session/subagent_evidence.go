package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"nekocode/bot/extension/tool/runtime/refbridge"
	"nekocode/bot/provider/types"
)

// SubagentEvidenceSource binds one committed attempt from session.json, without
// restoring active context or modifying the writer's transcript/index.
func SubagentEvidenceSource(ctx context.Context, parentID, id, attemptID string) (refbridge.Source, int, int, error) {
	if err := ctx.Err(); err != nil {
		return refbridge.Source{}, 0, 0, err
	}
	root, err := subagentRoot(parentID)
	if err != nil {
		return refbridge.Source{}, 0, 0, err
	}
	if err := validateID(id); err != nil {
		return refbridge.Source{}, 0, 0, err
	}
	if attemptID == "" {
		return refbridge.Source{}, 0, 0, fmt.Errorf("attempt ID required")
	}
	file, err := os.Open(filepath.Join(root, id, "session.json"))
	if err != nil {
		return refbridge.Source{}, 0, 0, err
	}
	defer file.Close()
	var header struct {
		ID            string            `json:"id"`
		FormatVersion int               `json:"format_version"`
		TranscriptSeq int               `json:"transcript_seq"`
		Subagent      *SubagentMetadata `json:"subagent"`
	}
	if err := json.NewDecoder(file).Decode(&header); err != nil {
		return refbridge.Source{}, 0, 0, err
	}
	if header.ID != id || header.Subagent == nil || header.Subagent.ParentSessionID != parentID || header.FormatVersion < 2 || header.FormatVersion > currentSnapshotVersion || header.TranscriptSeq < 0 {
		return refbridge.Source{}, 0, 0, fmt.Errorf("invalid child evidence scope")
	}
	var selected *SubagentAttempt
	for _, attempt := range header.Subagent.Attempts {
		if attempt.ID == attemptID {
			if selected != nil {
				return refbridge.Source{}, 0, 0, fmt.Errorf("ambiguous attempt")
			}
			copy := attempt
			selected = &copy
		}
	}
	if selected == nil {
		return refbridge.Source{}, 0, 0, os.ErrNotExist
	}
	first, last := selected.TranscriptStart, selected.TranscriptEnd
	if first < 1 || last < first-1 || last > header.TranscriptSeq {
		return refbridge.Source{}, 0, 0, fmt.Errorf("invalid committed attempt range")
	}
	source := refbridge.Source{Record: func(readCtx context.Context, sequence, through int) (types.Message, bool, error) {
		if err := readCtx.Err(); err != nil {
			return types.Message{}, false, err
		}
		if through != last || sequence < first || sequence > last {
			return types.Message{}, false, nil
		}
		page, err := subagentHistory(readCtx, parentID, id, sequence+1, 1)
		if err != nil {
			return types.Message{}, false, err
		}
		if page.End != sequence || page.Before != sequence || len(page.Messages) == 0 {
			return types.Message{}, false, fmt.Errorf("committed evidence record unavailable")
		}
		return page.Messages[len(page.Messages)-1], true, nil
	}}
	return source, first, last, nil
}
