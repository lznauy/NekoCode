package agent

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"nekocode/bot/policy"
	"nekocode/protocol"
)

type goalPreservationKey struct{}

// GoalPreservation carries the first observations and the closeout verdict for a goal run.
type GoalPreservation struct {
	Files       []protocol.PreservedFile
	Criteria    string
	Save        func([]protocol.PreservedFile) error
	NeedsReview bool
}

// WithGoalPreservation attaches the private working copy of the session goal.
// Newly observed baselines are committed before model or tool execution.
func WithGoalPreservation(ctx context.Context, goal *GoalPreservation) context.Context {
	return context.WithValue(ctx, goalPreservationKey{}, goal)
}

func (a *Agent) capturePreservedFiles(input string) error {
	if a.deps.toolRegistry == nil {
		return nil
	}
	root := a.deps.toolRegistry.Workspace().Primary()
	seen := make(map[string]bool, len(a.run.preserved))
	for _, file := range a.run.preserved {
		seen[file.Path] = true
	}
	for line := range strings.SplitSeq(input, "\n") {
		label, paths, ok := strings.Cut(strings.Replace(strings.TrimSpace(line), "：", ":", 1), ":")
		if !ok || (label != "保留原件" && !strings.EqualFold(label, "Preserve files")) {
			continue
		}
		for _, name := range strings.FieldsFunc(paths, func(r rune) bool { return strings.ContainsRune(",、，", r) }) {
			path := strings.TrimPrefix(strings.Trim(strings.TrimSpace(name), "`"), "./")
			if path == "" || seen[path] {
				continue
			}
			seen[path] = true
			sum, err := a.deps.toolExecutor.PreservedFileDigest(a.getCtx(), root, path)
			file := protocol.PreservedFile{Root: root, Path: path}
			if err == nil {
				file.SHA256 = hex.EncodeToString(sum[:])
			}
			a.run.preserved = append(a.run.preserved, file)
		}
	}
	goal, ok := a.getCtx().Value(goalPreservationKey{}).(*GoalPreservation)
	if !ok || len(a.run.preserved) == len(goal.Files) {
		return nil
	}
	if goal.Save != nil {
		if err := goal.Save(a.run.preserved); err != nil {
			return protocol.MarkEvidenceIncomplete(err)
		}
	}
	// A baseline is pending until the next preservation check. Steering
	// can arrive after the final check but before the last queue drain.
	goal.NeedsReview = true
	goal.Files = a.run.preserved
	return nil
}

func (r *turnRunner) reviewPreservedFiles(text *string, allowRetry bool) bool {
	a := r.agent
	var changed, unverifiable []string
	for _, file := range a.run.preserved {
		if file.SHA256 == "" {
			unverifiable = append(unverifiable, fmt.Sprintf("%q", file.Path))
			continue
		}
		sum, err := a.deps.toolExecutor.PreservedFileDigest(a.getCtx(), file.Root, file.Path)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			unverifiable = append(unverifiable, fmt.Sprintf("%q", file.Path))
			continue
		}
		if err != nil || hex.EncodeToString(sum[:]) != file.SHA256 {
			changed = append(changed, fmt.Sprintf("%q", file.Path))
		}
	}
	if goal, ok := a.getCtx().Value(goalPreservationKey{}).(*GoalPreservation); ok {
		goal.NeedsReview = len(changed) > 0 || len(unverifiable) > 0
	}
	if allowRetry && len(changed) > 0 && !a.run.preservationChecked && a.getCtx().Err() == nil {
		a.run.preservationChecked = true
		a.stream.discardText()
		a.clearFinalState()
		a.run.step++
		a.injectHint(&policy.Hint{Type: "preserve_files", Severity: "critical", Content: "交付前检查发现以下原件与任务开始时不同或无法读取：" + strings.Join(changed, ", ") + "。仅在有可靠的原始副本且修改来自本次任务时恢复；否则报告未解决的差异。"})
		return true
	}
	if len(changed) > 0 {
		*text += "\n\n原件校验未通过：" + strings.Join(changed, ", ")
	}
	if len(unverifiable) > 0 {
		*text += "\n\n无法校验原件：" + strings.Join(unverifiable, ", ")
	}
	return false
}
