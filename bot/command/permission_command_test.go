package command

import (
	"context"
	"strings"
	"testing"

	"nekocode/protocol"

	"nekocode/mode"
)

func TestPermissionCommandSwitchesMode(t *testing.T) {
	var full bool
	p := NewParser()
	RegisterDefaults(p, Deps{
		SetFullAccess: func(on bool) { full = on },
		GetFullAccess: func() bool { return full },
	})
	ctx := protocol.WithCommandStatusDisplay(context.Background())

	// Default: manual.
	out, handled := p.Execute(ctx, p.Parse("/permission"))
	if !handled || !strings.Contains(out, "manual") {
		t.Fatalf("status = %q", out)
	}

	// Successful switches update state without emitting a system message.
	out, _ = p.Execute(ctx, p.Parse("/permission full"))
	if !full {
		t.Fatal("SetFullAccess(true) was not applied")
	}
	if out != "" {
		t.Fatalf("successful full switch must be silent, got %q", out)
	}

	// Status now reports the full-takeover mode.
	if out, _ = p.Execute(ctx, p.Parse("/permission")); !strings.Contains(out, "全接管") {
		t.Fatalf("status after switch = %q", out)
	}

	// Back to manual.
	out, _ = p.Execute(ctx, p.Parse("/permission manual"))
	if full || out != "" {
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
	out, _ := p.Execute(protocol.WithCommandStatusDisplay(context.Background()), p.Parse("/permission auto"))
	if auto || !strings.Contains(out, "不可用") {
		t.Fatalf("auto enabled without judge: auto=%v output=%q", auto, out)
	}
	available = true
	out, _ = p.Execute(protocol.WithCommandStatusDisplay(context.Background()), p.Parse("/permission auto"))
	if !auto || out != "" {
		t.Fatalf("auto did not enable with judge: auto=%v output=%q", auto, out)
	}
}

func TestModeCommandSwitchIsSilent(t *testing.T) {
	var selected mode.ID
	h := New(Deps{SetInteractionMode: func(id mode.ID) error {
		if _, err := mode.Resolve(id); err != nil {
			return err
		}
		selected = id
		return nil
	}})
	out, handled := h.Execute(protocol.WithCommandStatusDisplay(context.Background()), "/mode plan", nil)
	if !handled || selected != mode.Plan || out != "" {
		t.Fatalf("switch: handled=%v selected=%q output=%q", handled, selected, out)
	}
	out, handled = h.Execute(protocol.WithCommandStatusDisplay(context.Background()), "/mode invalid", nil)
	if !handled || out == "" || selected != mode.Plan {
		t.Fatalf("invalid switch: handled=%v selected=%q output=%q", handled, selected, out)
	}
}

func TestPermissionFullReportsRiskWithoutStatusDisplay(t *testing.T) {
	var full bool
	p := NewParser()
	RegisterDefaults(p, Deps{
		GetFullAccess: func() bool { return full },
		SetFullAccess: func(value bool) { full = value },
	})
	out, handled := p.Execute(context.Background(), p.Parse("/permission full"))
	if !handled || !full || !strings.Contains(out, "免审批") || !strings.Contains(out, "/permission manual") {
		t.Fatalf("full access risk feedback = %q, handled=%v, full=%v", out, handled, full)
	}
}
