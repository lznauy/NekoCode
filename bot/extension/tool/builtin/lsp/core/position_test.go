package lspcore

import (
	"runtime"
	"strings"
	"testing"
)

func TestURIRoundtrip(t *testing.T) {
	paths := []string{"/home/u/a b.go", "/x/y.rs"}
	if runtime.GOOS == "windows" {
		paths = []string{`C:\Users\u\a b.go`, `D:\x\y.rs`}
	}
	for _, p := range paths {
		uri := pathToURI(p)
		if !strings.HasPrefix(uri, "file://") {
			t.Errorf("%q → %q is not a file URI", p, uri)
		}
		if got := uriToPath(uri); got != p {
			t.Errorf("roundtrip %q → %q", p, got)
		}
	}
}

func TestLocateEncoding(t *testing.T) {
	content := "package x\nαβ foo()\n" // line 2 has two 2-byte runes then a space
	u16, err := locate(content, 2, "foo", encodingUTF16)
	if err != nil {
		t.Fatal(err)
	}
	if u16.Line != 1 || u16.Character != 3 {
		t.Errorf("utf16 pos = %+v, want line 1 char 3", u16)
	}
	u8, err := locate(content, 2, "foo", encodingUTF8)
	if err != nil {
		t.Fatal(err)
	}
	if u8.Character != 5 {
		t.Errorf("utf8 char = %d, want 5", u8.Character)
	}
	if _, err := locate(content, 2, "missing", encodingUTF16); err == nil {
		t.Error("expected not-found error")
	}
	if _, err := locate(content, 99, "foo", encodingUTF16); err == nil {
		t.Error("expected out-of-range error")
	}
}

func TestLocateQualifiedSymbol(t *testing.T) {
	content := "package main\n\n\tgreet.Greet(\"x\")\n\tjson.loads('{}')\n"
	cases := []struct {
		line   int
		symbol string
		want   int
	}{
		{3, "greet.Greet", 7}, // tab + "greet."
		{3, "Greet", 7},       // same column without the qualifier
		{4, "json.loads", 6},  // tab + "json."
		{4, "loads", 6},
		{4, "pkg.loads", 6}, // qualifier differs from the line: fall back to "loads"
	}
	for _, c := range cases {
		pos, err := locate(content, c.line, c.symbol, encodingUTF16)
		if err != nil {
			t.Fatalf("locate(%q) line %d: %v", c.symbol, c.line, err)
		}
		if pos.Character != c.want {
			t.Errorf("locate(%q) line %d char = %d, want %d", c.symbol, c.line, pos.Character, c.want)
		}
	}
}

func TestLocateIdentifierBoundariesAndAmbiguity(t *testing.T) {
	for _, tc := range []struct {
		text, symbol string
		column       int
	}{
		{"foobar(foo)", "foo", 7},
		{"_foo + foo", "foo", 7},
		{"中foo + foo", "foo", 9},
		{"pkg.foobar(pkg.foo)", "pkg.foo", 15},
	} {
		pos, err := locate(tc.text, 1, tc.symbol, encodingUTF8)
		if err != nil || pos.Character != tc.column {
			t.Errorf("%q / %q: %+v %v", tc.text, tc.symbol, pos, err)
		}
	}
	for _, tc := range []struct{ text, symbol string }{{"foobar", "foo"}, {"foo + foo", "foo"}} {
		if _, err := locate(tc.text, 1, tc.symbol, encodingUTF8); err == nil {
			t.Errorf("unmatched or ambiguous symbol accepted: %+v", tc)
		}
	}
}

func TestLocateOccurrence(t *testing.T) {
	for _, tc := range []struct {
		text, symbol, encoding string
		occurrence, want       int
	}{
		{"foo(foo)", "foo", encodingUTF8, 1, 0},
		{"foo(foo)", "foo", encodingUTF8, 2, 4},
		{"α pkg.foo(pkg.foo)", "pkg.foo", encodingUTF16, 2, 14},
		{"α foo(foo)", "wrong.foo", encodingUTF16, 2, 6},
	} {
		pos, err := locate(tc.text, 1, tc.symbol, tc.encoding, tc.occurrence)
		if err != nil || pos.Character != tc.want {
			t.Errorf("%+v: pos=%+v err=%v", tc, pos, err)
		}
	}
	for _, value := range []int{-1, 3} {
		if _, err := locate("foo(foo)", 1, "foo", encodingUTF8, value); err == nil {
			t.Errorf("invalid occurrence accepted: %d", value)
		}
	}
}

func TestLocateOverlappingQualifiedOccurrences(t *testing.T) {
	if _, err := locate("a.a.a", 1, "a.a", encodingUTF8); err == nil {
		t.Fatal("overlapping qualified matches were not reported as ambiguous")
	}
	pos, err := locate("a.a.a", 1, "a.a", encodingUTF8, 2)
	if err != nil || pos.Character != 4 {
		t.Fatalf("second overlapping occurrence unavailable: pos=%+v err=%v", pos, err)
	}
	pos, err = locate("中.中.中", 1, "中.中", encodingUTF16, 2)
	if err != nil || pos.Character != 4 {
		t.Fatalf("Unicode overlap unavailable: pos=%+v err=%v", pos, err)
	}
}
