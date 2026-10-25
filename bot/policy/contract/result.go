package contract

import (
	"fmt"
	"strings"
)

func Control(r Result) (Decision, error) {
	d := Decision{Action: Continue}
	count := 0
	set := func(next Decision) { count++; d = next }
	if r.Decision != nil {
		set(*r.Decision)
	}
	if r.Stop != nil {
		set(Decision{Action: Terminate, StopReason: *r.Stop})
	}
	if r.BlockTool != nil {
		set(Decision{Action: Reject, Reason: r.BlockTool.Reason})
	}
	if r.BlockFinal != nil {
		set(Decision{Action: Reject, Reason: r.BlockFinal.Reason})
	}
	if r.RequireTool != nil {
		reason := r.RequireTool.Reason
		if r.RequireTool.Tool != "" {
			reason = "必须先调用 " + r.RequireTool.Tool + "：" + reason
		}
		set(Decision{Action: Reject, Reason: reason})
	}
	if count > 1 {
		return d, fmt.Errorf("multiple control decisions in one result")
	}
	switch d.Action {
	case Continue, Reject, Terminate:
	default:
		return d, fmt.Errorf("unknown decision action %q", d.Action)
	}
	if d.Action == Terminate && d.StopReason == "" {
		d.StopReason = StopPolicyBlocked
	}
	return d, nil
}

// Merge validates all controls and retains every hint and state update.
func Merge(results []Result) (Result, error) {
	out := Result{}
	selected := Decision{Action: Continue}
	var rejections []string
	rejected := false
	rank := func(a Action) int {
		switch a {
		case Terminate:
			return 2
		case Reject:
			return 1
		default:
			return 0
		}
	}
	for _, r := range results {
		if r.Err != nil {
			return Result{}, r.Err
		}
		d, err := Control(r)
		if err != nil {
			return Result{}, err
		}
		if d.Action == Reject {
			rejected = true
			if d.Reason != "" {
				rejections = append(rejections, d.Reason)
			}
		}
		if rank(d.Action) > rank(selected.Action) {
			selected = d
		} else if d.Action == selected.Action && d.Reason != "" {
			if selected.Reason != "" {
				selected.Reason += "; "
			}
			selected.Reason += d.Reason
		}
		// A terminal denial must survive subsequent merges with completed stops.
		if d.Action == Terminate && selected.Action == Terminate && terminalPriority(d.StopReason) > terminalPriority(selected.StopReason) {
			selected.StopReason = d.StopReason
		}
		if r.Hint != nil {
			out.Hints = append(out.Hints, *r.Hint)
		}
		out.Hints = append(out.Hints, r.Hints...)
		if r.StatePatch != nil {
			if out.StatePatch == nil {
				out.StatePatch = &StatePatch{Ints: map[string]int64{}, Strings: map[string]string{}}
			}
			for k, v := range r.StatePatch.Ints {
				out.StatePatch.Ints[k] = v
			}
			for k, v := range r.StatePatch.Strings {
				out.StatePatch.Strings[k] = v
			}
		}
	}
	// Stopping and approving the current output are independent decisions.
	// Encode a stopped rejection as policy_blocked so nested action/hook merges
	// cannot turn it back into successful completion.
	if selected.Action == Terminate && rejected {
		selected.StopReason = StopPolicyBlocked
		if reason := strings.Join(rejections, "; "); reason != "" {
			if selected.Reason != "" {
				selected.Reason += "; "
			}
			selected.Reason += reason
		}
	}
	if selected.Action != Continue {
		out.Decision = &selected
	}
	return out, nil
}

func terminalPriority(reason StopReason) int {
	switch reason {
	case StopPolicyBlocked:
		return 3
	case StopInterrupted:
		return 2
	case StopFormatError:
		return 1
	default:
		return 0
	}
}

func Validate(point string, r *Result) error {
	if r == nil {
		return nil
	}
	if r.Err != nil {
		return r.Err
	}
	d, err := Control(*r)
	if err != nil {
		return err
	}
	spec, _ := Point(point).Spec()
	if !spec.CanReject && d.Action == Reject {
		// Preserve legacy post-tool requirements as advisory hints.
		if (Point(point) == PostToolUse || Point(point) == PostToolBatch) && r.BlockTool == nil && r.Decision == nil {
			return nil
		}
		if Point(point) == PostToolUse || Point(point) == PostToolBatch {
			return fmt.Errorf("cannot reject a completed tool action")
		}
		return fmt.Errorf("%s is an observation event", point)
	}
	if !spec.CanStop && d.Action == Terminate {
		return fmt.Errorf("%s is an observation event", point)
	}
	if r.BlockTool != nil && point != "pre_tool_use" {
		return fmt.Errorf("block_tool is only valid at pre_tool_use")
	}
	if (r.BlockFinal != nil || r.RequireTool != nil) && point != "stop" && point != "post_tool_use" && point != "post_tool_batch" {
		return fmt.Errorf("final requirements are not valid at %s", point)
	}
	return nil
}

// Normalize applies event-specific legacy semantics before aggregation.
func Normalize(point, tool string, r Result) Result {
	if r.BlockTool != nil && r.BlockTool.Tool != "" && r.BlockTool.Tool != tool {
		r.BlockTool = nil
	}
	if (point == "post_tool_use" || point == "post_tool_batch") && (r.RequireTool != nil || r.BlockFinal != nil) {
		d, _ := Control(r)
		r.Hints = append(r.Hints, Hint{Type: "legacy_requirement", Severity: "critical", Content: d.Reason})
		r.RequireTool = nil
		r.BlockFinal = nil
	}
	return r
}
