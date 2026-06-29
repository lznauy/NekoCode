package builtin

import "testing"

func TestClaimsTestsPassedNegation(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"测试通过", true},
		{"测试未通过", false},
		{"测试没有通过", false},
		{"tests passed", true},
		{"tests did not pass", false},
		{"all green", true},
		{"测试都绿了", true},
		{"一次过", true},
		{"还没测试", false},
		{"", false},
	}
	for _, c := range cases {
		if got := claimsTestsPassed(c.text); got != c.want {
			t.Errorf("claimsTestsPassed(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

func TestClaimsCompletedNegation(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"已完成", true},
		{"尚未完成", false},
		{"没有完成", false},
		{"completed", true},
		{"not completed", false},
		{"已修复", true},
		{"未能修复", false},
		{"", false},
	}
	for _, c := range cases {
		if got := claimsCompleted(c.text); got != c.want {
			t.Errorf("claimsCompleted(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

func TestMentionsUnverified(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"未验证", true},
		{"未能验证", true},
		{"没有验证", true},
		{"unable to verify", true},
		{"not verified", true},
		{"已验证通过", false},
		{"测试通过", false},
		{"", false},
	}
	for _, c := range cases {
		if got := mentionsUnverified(c.text); got != c.want {
			t.Errorf("mentionsUnverified(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

func TestMentionsFailure(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"有一次失败", true},
		{"报错了", true},
		{"failed", true},
		{"error occurred", true},
		{"全程顺利", false},
		{"", false},
	}
	for _, c := range cases {
		if got := mentionsFailure(c.text); got != c.want {
			t.Errorf("mentionsFailure(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}
