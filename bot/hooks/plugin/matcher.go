package plugin

import (
	"regexp"
	"strings"
	"sync"
)

var matcherCache sync.Map

func matchTool(matcher, toolName string) bool {
	if matcher == "" || matcher == ".*" {
		return true
	}
	for alt := range strings.SplitSeq(matcher, "|") {
		alt = strings.TrimSpace(alt)
		if alt == toolName {
			return true
		}
		re := getOrCompileMatcher(alt)
		if re != nil && re.MatchString(toolName) {
			return true
		}
	}
	return false
}

func getOrCompileMatcher(pattern string) *regexp.Regexp {
	if v, ok := matcherCache.Load(pattern); ok {
		return v.(*regexp.Regexp)
	}
	re, err := regexp.Compile("^" + pattern + "$")
	if err != nil {
		return nil
	}
	matcherCache.Store(pattern, re)
	return re
}
