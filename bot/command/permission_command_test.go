package command

import (
	"context"
	"strings"
	"testing"
)

func TestPermissionCommandSwitchesMode(t *testing.T) {
	var full bool
	p := NewParser()
	RegisterDefaults(p, Deps{
		SetFullAccess: func(on bool) { full = on },
		GetFullAccess: func() bool { return full },
	})
	ctx := context.Background()

	// Default: manual.
	out, handled := p.Execute(ctx, p.Parse("/permission"))
	if !handled || !strings.Contains(out, "manual") {
		t.Fatalf("status = %q", out)
	}

	// Switch to full: flag flips and the risk warning is shown.
	out, _ = p.Execute(ctx, p.Parse("/permission full"))
	if !full {
		t.Fatal("SetFullAccess(true) was not applied")
	}
	if !strings.Contains(out, "全接管") || !strings.Contains(out, "/permission manual") {
		t.Fatalf("full mode must come back with a risk warning, got %q", out)
	}

	// Status now reports the full-takeover mode.
	if out, _ = p.Execute(ctx, p.Parse("/permission")); !strings.Contains(out, "全接管") {
		t.Fatalf("status after switch = %q", out)
	}

	// Back to manual.
	out, _ = p.Execute(ctx, p.Parse("/permission manual"))
	if full || !strings.Contains(out, "手动审批") {
		t.Fatalf("switch back failed: full=%v out=%q", full, out)
	}

	// Unknown mode shows usage.
	if out, _ = p.Execute(ctx, p.Parse("/permission yolo")); !strings.Contains(out, "Usage") {
		t.Fatalf("unknown mode = %q", out)
	}
}

func TestPermissionCommandUnavailableWithoutDeps(t *testing.T) {
	p := NewParser()
	RegisterDefaults(p, Deps{})
	out, handled := p.Execute(context.Background(), p.Parse("/permission full"))
	if !handled || !strings.Contains(out, "unavailable") {
		t.Fatalf("missing deps = %q", out)
	}
}

func TestPermissionAutoRequiresAvailableJudge(t *testing.T) {
	auto := false
	available := false
	p := NewParser()
	RegisterDefaults(p, Deps{
		SetFullAccess: func(bool) {}, GetFullAccess: func() bool { return false },
		SetBashAuto: func(on bool) { auto = on }, GetBashAuto: func() bool { return auto },
		CanBashAuto: func() bool { return available },
	})
	out, _ := p.Execute(context.Background(), p.Parse("/permission auto"))
	if auto || !strings.Contains(out, "不可用") {
		t.Fatalf("auto enabled without judge: auto=%v output=%q", auto, out)
	}
	available = true
	out, _ = p.Execute(context.Background(), p.Parse("/permission auto"))
	if !auto || !strings.Contains(out, "已切换 auto") {
		t.Fatalf("auto did not enable with judge: auto=%v output=%q", auto, out)
	}
}
