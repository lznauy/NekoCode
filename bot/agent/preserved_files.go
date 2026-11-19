package agent

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"nekocode/bot/policy"
)

type preservedFile struct {
	root   string
	path   string
	digest [sha256.Size]byte
	known  bool
}

func (a *Agent) capturePreservedFiles(input string) []preservedFile {
	root := a.deps.toolRegistry.Workspace().Primary()
	var files []preservedFile
	seen := map[string]bool{}
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
			files = append(files, preservedFile{root: root, path: path, digest: sum, known: err == nil})
		}
	}
	return files
}

func (r *turnRunner) reviewPreservedFiles(text *string, allowRetry bool) bool {
	a := r.agent
	var changed, unverifiable []string
	for _, file := range a.run.preserved {
		if !file.known {
			unverifiable = append(unverifiable, fmt.Sprintf("%q", file.path))
			continue
		}
		sum, err := a.deps.toolExecutor.PreservedFileDigest(a.getCtx(), file.root, file.path)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			unverifiable = append(unverifiable, fmt.Sprintf("%q", file.path))
			continue
		}
		if err != nil || sum != file.digest {
			changed = append(changed, fmt.Sprintf("%q", file.path))
		}
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
