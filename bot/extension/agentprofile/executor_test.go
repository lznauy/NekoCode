package agentprofile

import (
	"context"
	"nekocode/mode"
	"testing"
)

func TestExecutorRequiresTrustedCoordinatorSource(t *testing.T) {
	p := Executor([]string{"read", "shell"})
	for _, id := range []mode.ID{mode.Standard, mode.Plan} {
		d, _ := mode.Resolve(id)
		if err := p.CheckSource(mode.WithSnapshot(t.Context(), d.Snapshot)); err == nil {
			t.Fatalf("%s invoked executor", id)
		}
	}
	if err := p.CheckSource(context.Background()); err == nil {
		t.Fatal("missing origin invoked executor")
	}
	d, _ := mode.Resolve(mode.SuperAgent)
	if err := p.CheckSource(mode.WithSnapshot(t.Context(), d.Snapshot)); err != nil {
		t.Fatal(err)
	}
	forged := d.Snapshot
	forged.ToolPolicy = mode.AllTools
	if err := p.CheckSource(mode.WithSnapshot(t.Context(), forged)); err == nil {
		t.Fatal("forged role snapshot invoked executor")
	}
	if Executor(p.Tools).ConfigurationHash() == (Profile{Name: "executor", Tools: p.Tools}).ConfigurationHash() {
		t.Fatal("required origin missing from configuration hash")
	}
}
