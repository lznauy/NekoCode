package sse

import "testing"

func TestSSELineData(t *testing.T) {
	for _, tc := range []struct {
		line, want string
		ok         bool
	}{
		{"data:hello", "hello", true}, {"data: hello", "hello", true},
		{"data:", "", true}, {"data", "", true}, {"data:  hello", " hello", true},
		{"event:hello", "", false}, {"database:hello", "", false},
	} {
		got, ok := SSELineData(tc.line)
		if got != tc.want || ok != tc.ok {
			t.Errorf("SSELineData(%q) = (%q, %v), want (%q, %v)", tc.line, got, ok, tc.want, tc.ok)
		}
	}
}
