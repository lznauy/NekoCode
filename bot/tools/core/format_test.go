package core

import "testing"

func TestToToolDefs(t *testing.T) {
	defs := ToToolDefs(nil)
	if len(defs) != 0 {
		t.Error("expected empty")
	}

	descs := []Descriptor{{
		Name: "test", Description: "a test tool",
		Parameters: []Parameter{
			{Name: "path", Type: "string", Required: true, Description: "file path"},
			{Name: "depth", Type: "integer", Required: false, Description: "how deep"},
		},
	}}
	defs = ToToolDefs(descs)
	if len(defs) != 1 {
		t.Fatal("expected 1 def")
	}
	d := defs[0]
	if d.Type != "function" {
		t.Error("bad type")
	}
	if d.Function.Name != "test" {
		t.Error("bad name")
	}
	if d.Function.Parameters.Type != "object" {
		t.Error("bad params type")
	}
	if d.Function.Parameters.Required[0] != "path" {
		t.Error("bad required")
	}
	if len(d.Function.Parameters.Properties) != 2 {
		t.Error("bad props count")
	}
}

func TestFormatArgs(t *testing.T) {
	got := FormatArgs(map[string]any{
		"b":             "plain",
		"a":             "x,y",
		"_preview":      "hidden",
		"_sub_callback": "hidden",
	})
	want := `a="x,y",b=plain`
	if got != want {
		t.Fatalf("FormatArgs() = %q, want %q", got, want)
	}
}

func TestToolCallResultEffectiveOutput(t *testing.T) {
	if got := (ToolCallResult{Output: "ok"}).EffectiveOutput(); got != "ok" {
		t.Fatalf("EffectiveOutput() = %q, want ok", got)
	}
	if got := (ToolCallResult{Output: "ok", Error: "bad"}).EffectiveOutput(); got != "bad" {
		t.Fatalf("EffectiveOutput() = %q, want bad", got)
	}
}
