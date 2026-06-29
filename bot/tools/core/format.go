package core

import (
	"fmt"
	"sort"
	"strings"

	"nekocode/bot/llm/types"
)

func ToToolDefs(descs []Descriptor) []types.ToolDef {
	defs := make([]types.ToolDef, len(descs))
	for i, d := range descs {
		props := make(map[string]types.Property)
		var required []string
		for _, p := range d.Parameters {
			props[p.Name] = types.Property{Type: p.Type, Description: p.Description}
			if p.Required {
				required = append(required, p.Name)
			}
		}
		defs[i] = types.ToolDef{
			Type: "function",
			Function: types.FunctionDef{
				Name: d.Name, Description: d.Description,
				Parameters: types.Parameters{Type: "object", Properties: props, Required: required},
			},
		}
	}
	return defs
}

// FormatArgs serializes a tool args map into "key=value,key2=value2" form.
func FormatArgs(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		if k == "_preview" || k == "_sub_callback" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var pairs []string
	for _, k := range keys {
		val := fmt.Sprint(args[k])
		if strings.ContainsAny(val, ",="+"\"") {
			val = "\"" + strings.ReplaceAll(strings.ReplaceAll(val, "\\", "\\\\"), "\"", "\\\"") + "\""
		}
		pairs = append(pairs, k+"="+val)
	}
	return strings.Join(pairs, ",")
}
