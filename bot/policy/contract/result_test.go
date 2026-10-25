package contract

import "testing"

func TestTerminalDenialSurvivesNestedMerges(t *testing.T) {
	deny := Result{Decision: &Decision{Action: Reject, Reason: "denied"}}
	complete := Result{Decision: &Decision{Action: Terminate, StopReason: StopCompleted}}
	for _, pair := range [][]Result{{deny, complete}, {complete, deny}} {
		merged, err := Merge(pair)
		if err != nil {
			t.Fatal(err)
		}
		for _, outer := range [][]Result{{merged, complete}, {complete, merged}} {
			final, err := Merge(outer)
			if err != nil {
				t.Fatal(err)
			}
			d, err := Control(final)
			if err != nil {
				t.Fatal(err)
			}
			if d.Action != Terminate || d.StopReason != StopPolicyBlocked {
				t.Fatalf("denial lost: %+v", d)
			}
		}
	}
}

func TestStateQuotaBoundaries(t *testing.T) {
	state := StatePatch{Strings: map[string]string{"x": string(make([]byte, MaxStateBytes-1))}}
	if err := ValidateState(state); err != nil {
		t.Fatal(err)
	}
	state.Strings["x"] += "x"
	if err := ValidateState(state); err == nil {
		t.Fatal("accepted state above byte limit")
	}
}

func FuzzMergeSafety(f *testing.F) {
	f.Add([]byte{1, 2, 0, 3})
	f.Add([]byte{2, 1, 2})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 32 {
			return
		}
		var results []Result
		hasReject, hasStop := false, false
		for _, v := range data {
			r := Result{Hints: []Hint{{Content: "hint"}}}
			switch v % 4 {
			case 1:
				r.Decision = &Decision{Action: Reject, Reason: "denied"}
				hasReject = true
			case 2:
				r.Decision = &Decision{Action: Terminate, StopReason: StopCompleted}
				hasStop = true
			case 3:
				r.Decision = &Decision{Action: Terminate, StopReason: StopPolicyBlocked}
				hasStop = true
			}
			results = append(results, r)
		}
		merged, err := Merge(results)
		if err != nil {
			t.Fatal(err)
		}
		d, err := Control(merged)
		if err != nil {
			t.Fatal(err)
		}
		if hasReject && hasStop && (d.Action != Terminate || d.StopReason != StopPolicyBlocked) {
			t.Fatalf("denial lost: %+v", d)
		}
		if len(merged.Hints) != len(results) {
			t.Fatal("hints lost")
		}
		var nested Result
		for _, r := range results {
			nested, err = Merge([]Result{nested, r})
			if err != nil {
				t.Fatal(err)
			}
		}
		nd, err := Control(nested)
		if err != nil {
			t.Fatal(err)
		}
		if nd.Action != d.Action || nd.StopReason != d.StopReason {
			t.Fatalf("nested merge changes safety: %+v != %+v", nd, d)
		}
	})
}
