package mode

import (
	"context"
	"testing"
)

func TestRegistryIsImmutableAndRejectsAmbiguity(t *testing.T) {
	definitions := Definitions()
	original := definitions[0]
	if _, err := NewRegistry(original, original); err == nil {
		t.Fatal("duplicate accepted")
	}
	if _, err := NewRegistry(Definition{}); err == nil {
		t.Fatal("incomplete definition accepted")
	}
	definitions[0].Label = "changed"
	resolved, err := Resolve(Standard)
	if err != nil || resolved.Label != original.Label {
		t.Fatalf("registry mutated: %+v %v", resolved, err)
	}
	custom := original
	custom.ID, custom.Label, custom.Strategy = "custom", "Custom", "delegated"
	registry, err := NewRegistry(original, custom)
	if err != nil {
		t.Fatal(err)
	}
	if d, err := registry.Resolve("custom"); err != nil || d.Strategy != "delegated" {
		t.Fatalf("extension lost: %+v %v", d, err)
	}
	if _, err := Resolve("super-agent"); err == nil {
		t.Fatal("unimplemented mode exposed")
	}
}

func TestSnapshotsFailClosedAndDoNotFollowSelection(t *testing.T) {
	plan, _ := Resolve(Plan)
	ctx := WithSnapshot(context.Background(), plan.Snapshot)
	changed := plan.Snapshot
	changed.ToolPolicy = AllTools
	if _, err := Validate(changed); err == nil {
		t.Fatal("modified tool ceiling accepted")
	}
	changed = plan.Snapshot
	changed.Version++
	if _, err := Validate(changed); err == nil {
		t.Fatal("unknown policy version accepted")
	}
	captured, ok := FromContext(ctx)
	if !ok || captured != plan.Snapshot {
		t.Fatalf("captured policy changed: %+v", captured)
	}
	if d, err := Resolve(""); err != nil || d.ID != Standard {
		t.Fatal("legacy default changed")
	}
}

func TestOneShotPlanSyntax(t *testing.T) {
	for _, input := range []string{"/plan inspect", "  /PLAN\tinspect\ncode"} {
		if !OneShot(input) {
			t.Fatalf("missed %q", input)
		}
	}
	for _, input := range []string{"/plan", "/plan ", "/planner inspect", "please /plan inspect"} {
		if OneShot(input) {
			t.Fatalf("misclassified %q", input)
		}
	}
}
