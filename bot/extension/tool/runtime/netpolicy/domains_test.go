package netpolicy

import (
	"reflect"
	"testing"
)

func TestDomains(t *testing.T) {
	got, err := Normalize([]string{"EXAMPLE.COM.", "例子.中国", "example.com"})
	if err != nil || !reflect.DeepEqual(got, []string{"example.com", "xn--fsqu00a.xn--fiqs8s"}) {
		t.Fatalf("%v %v", got, err)
	}
	for _, domain := range []string{"", "*.example.com", "https://example.com", "example.com:443", "127.0.0.1", "::1", "a..com", "-a.com", "example.com/path", "foo_bar.com"} {
		if _, err := Normalize([]string{domain}); err == nil {
			t.Errorf("accepted %q", domain)
		}
	}
	for _, raw := range []any{nil, "example.com", []any{123}} {
		if _, err := Parse(raw); err == nil {
			t.Errorf("accepted %+v", raw)
		}
	}
	if got, err := Parse([]any{}); err != nil || got == nil {
		t.Fatal("empty set lost")
	}
	if Intersect([]string{"a.com"}, []string{"b.com"}) == nil {
		t.Fatal("intersection became unrestricted")
	}
	if Covers([]string{}, nil) || !Covers(nil, []string{"a.com"}) || Covers([]string{"a.com"}, []string{"b.com"}) {
		t.Fatal("coverage widened")
	}
}
