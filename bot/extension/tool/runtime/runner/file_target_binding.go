package runner

import (
	"errors"
	"fmt"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/bot/extension/tool/runtime/workspace"
	"os"
	"path/filepath"
)

var errFileTargetChanged = errors.New("file target changed before execution; review and approve it again; do not retry automatically")

// Workspace access replaces ordinary file arguments with their canonical paths.
// Keep that exact value across approval rather than resolving a new target and
// silently authorizing it. Image understanding instead uses no-follow opens and
// an ordered content digest covering every image in the batch.
type boundFileTarget struct {
	path      string
	canonical string
}

func fileTargetBinding(tc core.ToolCallItem) []boundFileTarget {
	if tc.Name == "image_understand" {
		return nil
	}
	var paths []boundFileTarget
	for _, target := range permission.FileToolTargets(tc.Name, tc.Args) {
		paths = append(paths, boundFileTarget{path: target.Path, canonical: target.Path})
	}
	return paths
}

func validateFileTargetBinding(expected []boundFileTarget) error {
	for _, target := range expected {
		actual, err := workspace.Resolve(target.path)
		if err != nil {
			return fmt.Errorf("%w: %v", errFileTargetChanged, err)
		}
		if actual != target.canonical {
			return errFileTargetChanged
		}
	}
	return nil
}

// Managed image output remains implicit at execution so it retains the tool's
// managed-directory access. Only policy and approval see its canonical path.
// A bare resume call downloads to the destination saved with the receipt, so
// policy must evaluate that directory instead of the managed default.
func (e *Executor) bindFilePolicyCall(tc core.ToolCallItem) (core.ToolCallItem, []boundFileTarget, error) {
	targets := fileTargetBinding(tc)
	if tc.Name != "image_gen" || len(targets) > 0 {
		return tc, targets, nil
	}
	if dir, explicit := e.imageResumeOutputDir(tc.Args); explicit && dir != "" {
		policyCall := tc
		policyCall.Args = permission.SetFileTarget(tc.Args, permission.FileTarget{Key: "output_dir"}, dir)
		return policyCall, []boundFileTarget{{path: dir, canonical: dir}}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return tc, nil, err
	}
	path := filepath.Join(home, ".nekocode", "images")
	canonical, err := workspace.Resolve(path)
	if err != nil {
		return tc, nil, err
	}
	policyCall := tc
	policyCall.Args = permission.SetFileTarget(tc.Args, permission.FileTarget{Key: "output_dir"}, canonical)
	return policyCall, []boundFileTarget{{path: path, canonical: canonical}}, nil
}

// imageResumeOutputDir reports the destination saved with an image generation
// receipt when args is a bare resume call. The registry lookup lets policy see
// the resume destination even after the tool registry was rebuilt.
func (e *Executor) imageResumeOutputDir(args map[string]any) (string, bool) {
	id, _ := args["resume_id"].(string)
	if id == "" || len(args) != 1 {
		return "", false
	}
	entry, err := e.registry.Lookup("image_gen")
	if err != nil {
		return "", false
	}
	resume, ok := entry.Tool.(interface {
		ResumeOutputDir(id string) (string, bool)
	})
	if !ok {
		return "", false
	}
	return resume.ResumeOutputDir(id)
}
