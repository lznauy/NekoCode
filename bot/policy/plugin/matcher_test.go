package plugin

import "testing"

func TestCompileMatcher(t *testing.T) {
	for _, tc := range []struct {
		pattern, tool string
		want          bool
	}{
		{"", "anytool", true},
		{".*", "anytool", true},
		{"web_.*", "web_search", true},
		{"read", "read", true},
		{"read", "write", false},
		{"read|write", "write", true},
		{"read|write", "overwrite", false},
		{"read_(file|dir)", "read_file", true},
		{"read_(file|dir)", "read_dir", true},
	} {
		t.Run(tc.pattern+"/"+tc.tool, func(t *testing.T) {
			matcher, err := compileMatcher(tc.pattern)
			if err != nil {
				t.Fatal(err)
			}
			if got := matcher.MatchString(tc.tool); got != tc.want {
				t.Fatalf("match=%v, want %v", got, tc.want)
			}
		})
	}
	if _, err := compileMatcher("["); err == nil {
		t.Fatal("invalid pattern accepted")
	}
}
