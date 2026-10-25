package plugin

import "regexp"

func compileMatcher(matcher string) (*regexp.Regexp, error) {
	if matcher == "" {
		matcher = ".*"
	}
	return regexp.Compile("^(?:" + matcher + ")$")
}
