package runner

import (
	"errors"
	"fmt"

	"nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/mode"
)

func (e *Executor) toolPolicy() mode.ToolPolicy {
	e.fnMu.RLock()
	defer e.fnMu.RUnlock()
	return e.interactionMode.ToolPolicy
}

// checkToolPolicy enforces capabilities using an atomic registered entry.
func (e *Executor) checkToolPolicy(name string, entry tools.Entry) error {
	policy := e.toolPolicy()
	if entry.CoordinatorOnly && policy != mode.CoordinationTools {
		return errors.New("tool requires coordinator mode")
	}
	switch policy {
	case "", mode.AllTools:
		return nil
	case mode.ReadOnlyTools:
		if e.planAllows(name, entry.PlanAllowed) {
			return nil
		}
		return errors.New("plan mode: blocked")
	case mode.CoordinationTools:
		if entry.CoordinatorAllowed {
			return nil
		}
		return errors.New("super-agent mode: direct execution blocked; delegate this work using task_submit")
	default:
		return fmt.Errorf("unsupported tool policy %q", policy)
	}
}

// CheckToolPolicy applies the mode ceiling before hooks or task admission.
// Dispatch and previews repeat this check at their own execution boundaries.
func (e *Executor) CheckToolPolicy(name string) error {
	entry, err := e.registry.Lookup(name)
	if err != nil {
		// Preserve Standard's governance-before-dispatch handling of unknown tools.
		if policy := e.toolPolicy(); policy == "" || policy == mode.AllTools {
			return nil
		}
		return err
	}
	return e.checkToolPolicy(name, entry)
}

// ToolDescriptors excludes execution tools from the coordinator's model view.
func (e *Executor) ToolDescriptors() []core.Descriptor {
	policy := e.toolPolicy()
	return e.registry.DescriptorsMatching(func(entry tools.Entry) bool {
		// Preserve the established Standard/Plan view for other tools.
		if policy != mode.CoordinationTools {
			return !entry.CoordinatorOnly
		}
		return e.checkToolPolicy(entry.Tool.Name(), entry) == nil
	})
}
