package contract

import "testing"

// These permissions are compatibility boundaries, not a list generated from
// the registry: adding a point must choose its control behavior explicitly.
func TestPointControlCompatibility(t *testing.T) {
	guards := map[Point]bool{UserSubmit: true, PreModel: true, PreToolUse: true, Stop: true}
	post := map[Point]bool{PostToolUse: true, PostToolBatch: true}
	points := Points()
	if len(points) != 14 {
		t.Fatalf("unexpected canonical point count: %d", len(points))
	}
	names, ids := map[string]bool{}, map[Point]bool{}
	for _, spec := range points {
		if spec.Name == "" || names[spec.Name] || ids[spec.Point] {
			t.Fatalf("invalid or duplicate event: %+v", spec)
		}
		names[spec.Name], ids[spec.Point] = true, true
		if !KnownPoint(spec.Point) {
			t.Fatalf("unrecognized point: %s", spec.Point)
		}
		for _, action := range []Action{Continue, Reject, Terminate} {
			err := Validate(string(spec.Point), &Result{Decision: &Decision{Action: action}})
			allowed := action == Continue || guards[spec.Point] || action == Terminate && post[spec.Point]
			if (err == nil) != allowed {
				t.Errorf("%s %s allowed=%v error=%v", spec.Point, action, allowed, err)
			}
		}
		legacy := &Result{RequireTool: &RequireTool{Reason: "read first"}}
		if err := Validate(string(spec.Point), legacy); (err == nil) != (spec.Point == Stop || post[spec.Point]) {
			t.Errorf("legacy requirements at %s: %v", spec.Point, err)
		}
	}
	points[0].Name = "mutated"
	if Points()[0].Name == "mutated" {
		t.Fatal("caller can change registry")
	}
	if KnownPoint("unknown") {
		t.Fatal("unknown point accepted")
	}
}
