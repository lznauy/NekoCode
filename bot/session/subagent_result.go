package session

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nekocode/bot/extension/tool/runtime/taskbridge"
)

const resultRefPrefix = "task-result.v1."

type taskResultIdentity struct {
	Parent  string `json:"parent"`
	Task    string `json:"task"`
	Attempt string `json:"attempt"`
	Status  string `json:"status"`
	Digest  string `json:"sha256"`
}

// ReadSubagentResult reads canonical terminal metadata, never observation.json
// or arbitrary paths. The reference pins parent/task/attempt/content together;
// it identifies evidence and grants no authority or permission to execute it.
func ReadSubagentResult(ctx context.Context, parentID string, q taskbridge.ResultQuery) (taskbridge.ResultPage, error) {
	fail := func(err error) (taskbridge.ResultPage, error) { return taskbridge.ResultPage{}, err }
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if q.Limit < 1 || q.Limit > taskbridge.MaxResultPage || q.Offset < 0 {
		return fail(fmt.Errorf("invalid result offset or limit"))
	}
	if q.Offset > 0 && q.Ref == "" {
		return fail(fmt.Errorf("use result_ref from the first page to continue"))
	}
	var pinned *taskResultIdentity
	if q.Ref != "" {
		if len(q.Ref) > 4096 || !strings.HasPrefix(q.Ref, resultRefPrefix) {
			return fail(fmt.Errorf("invalid result reference"))
		}
		raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(q.Ref, resultRefPrefix))
		if err != nil {
			return fail(fmt.Errorf("invalid result reference"))
		}
		var ref taskResultIdentity
		if json.Unmarshal(raw, &ref) != nil || ref.Parent != parentID || (q.ID != "" && q.ID != ref.Task) || (q.AttemptID != "" && q.AttemptID != ref.Attempt) {
			return fail(fmt.Errorf("result reference does not match this parent/task/attempt"))
		}
		q.ID, q.AttemptID = ref.Task, ref.Attempt
		pinned = &ref
	}
	root, err := subagentRoot(parentID)
	if err != nil {
		return fail(err)
	}
	if err := validateID(q.ID); err != nil {
		return fail(err)
	}
	if len(q.ID) > 255 || len(q.AttemptID) > 255 {
		return fail(fmt.Errorf("invalid result identity"))
	}
	file, err := os.Open(filepath.Join(root, q.ID, "session.json"))
	if err != nil {
		return fail(err)
	}
	defer file.Close()
	var header struct {
		ID            string            `json:"id"`
		FormatVersion int               `json:"format_version"`
		Subagent      *SubagentMetadata `json:"subagent"`
	}
	if err := json.NewDecoder(file).Decode(&header); err != nil {
		return fail(err)
	}
	if header.ID != q.ID || header.Subagent == nil || header.Subagent.ParentSessionID != parentID || header.FormatVersion < 2 || header.FormatVersion > currentSnapshotVersion {
		return fail(fmt.Errorf("invalid subagent result ownership or format"))
	}
	meta := header.Subagent
	if q.AttemptID == "" {
		q.AttemptID = meta.CurrentAttemptID
	}
	var selected *SubagentAttempt
	for i := range meta.Attempts {
		if meta.Attempts[i].ID == q.AttemptID {
			selected = &meta.Attempts[i]
			break
		}
	}
	if selected == nil {
		return fail(fmt.Errorf("attempt not found; use task_list latest_attempt.id or a previous result_ref"))
	}
	switch selected.Status {
	case "completed", "partial", "failed", "interrupted":
	default:
		return fail(fmt.Errorf("task result is not final (%s); do not poll a running task", selected.Status))
	}
	if selected.Result == "" {
		return fail(fmt.Errorf("attempt has no saved result"))
	}
	digest := sha256.Sum256([]byte(selected.Result))
	identity := taskResultIdentity{Parent: parentID, Task: q.ID, Attempt: selected.ID, Status: selected.Status, Digest: hex.EncodeToString(digest[:])}
	if pinned != nil && *pinned != identity {
		return fail(fmt.Errorf("result changed since this reference was issued; inspect a fresh first page"))
	}
	raw, err := json.Marshal(identity)
	if err != nil {
		return fail(err)
	}
	text := []rune(selected.Result)
	if q.Offset > len(text) {
		return fail(fmt.Errorf("result offset exceeds total"))
	}
	end := q.Offset + min(q.Limit, len(text)-q.Offset)
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	isolation := meta.Isolation
	if isolation == "" {
		isolation = "shared"
	}
	return taskbridge.ResultPage{WorkspaceBase: meta.WorkspaceBase, SourceHead: meta.SourceHead, WorkspaceKind: meta.WorkspaceKind, Isolation: isolation, ID: q.ID, AttemptID: selected.ID, Ref: resultRefPrefix + base64.RawURLEncoding.EncodeToString(raw), Status: selected.Status, Content: string(text[q.Offset:end]), Offset: q.Offset, NextOffset: end, Total: len(text), HasMore: end < len(text)}, nil
}
