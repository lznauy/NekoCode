package tools

import (
	"reflect"
	"testing"

	"nekocode/bot/tools/core"
)

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

func TestFormatArgsFacade(t *testing.T) {
	got := FormatArgs(map[string]any{"b": "plain", "a": "x,y", "_preview": "hidden"})
	if got != `a="x,y",b=plain` {
		t.Fatalf("FormatArgs() = %q", got)
	}
}

func TestCoreAliases(t *testing.T) {
	if reflect.TypeOf(ToolCallItem{}) != reflect.TypeOf(core.ToolCallItem{}) {
		t.Fatal("ToolCallItem facade is not the core type")
	}
	if reflect.TypeOf(Descriptor{}) != reflect.TypeOf(core.Descriptor{}) {
		t.Fatal("Descriptor facade is not the core type")
	}
	if ModeParallel == ModeSequential {
		t.Fatal("execution mode aliases collapsed")
	}
}
