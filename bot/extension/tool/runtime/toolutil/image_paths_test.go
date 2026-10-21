package toolutil

import "testing"

func TestImageUnderstandingPaths(t *testing.T) {
	for _, args := range []map[string]any{
		{}, {"path": ""}, {"path": 1}, {"paths": "a"}, {"paths": []any{}},
		{"paths": []any{"a", 2}}, {"paths": []string{"a", " "}},
		{"paths": []string{"1", "2", "3", "4", "5"}}, {"path": "a", "paths": []string{"b"}},
	} {
		if _, err := ImageUnderstandingPaths(args); err == nil {
			t.Fatalf("accepted %#v", args)
		}
	}
	for _, args := range []map[string]any{
		{"path": " a "}, {"paths": []string{"a", "b"}}, {"paths": []any{"a", "b"}},
	} {
		paths, err := ImageUnderstandingPaths(args)
		if err != nil || paths[0] != "a" {
			t.Fatalf("paths=%v err=%v", paths, err)
		}
	}
}
