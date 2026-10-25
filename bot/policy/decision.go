package policy

import (
	"fmt"
	"nekocode/bot/policy/contract"
)

// Outcome is the only control projection consumed by the runtime. All hints
// survive aggregation; terminal decisions also retain any output rejection.
type Outcome struct {
	Decision
	Hints []Hint
}

func Resolve(results []Result) Outcome {
	merged, err := contract.Merge(results)
	if err != nil {
		return Outcome{Decision: Decision{Action: Terminate, StopReason: StopPolicyBlocked, Reason: err.Error()}, Hints: []Hint{{Type: "hook_error", Severity: "warning", Content: err.Error()}}}
	}
	d, _ := contract.Control(merged)
	return Outcome{Decision: d, Hints: merged.Hints}
}

func hookFailure(h Hook, err error) *Result {
	r := &Result{Hint: &Hint{Type: "hook_error", Severity: "warning", Content: fmt.Sprintf("Hook %q: %v", h.Name, err)}}
	if h.Guard {
		a := Terminate
		if spec, _ := h.Point.Spec(); spec.CanReject {
			a = Reject
		}
		r.Decision = &Decision{Action: a, Reason: r.Hint.Content, StopReason: StopPolicyBlocked}
	}
	return r
}
