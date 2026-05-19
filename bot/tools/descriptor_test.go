package tools

import (
	"testing"
)

func TestToToolDefs(t *testing.T) {
	// Empty.
	defs := ToToolDefs(nil)
	if len(defs) != 0 {
		t.Error("expected empty")
	}

	// With required params.
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
