package wecom

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// splitMessage preserves the original text. Formatting is not a prerequisite
// for delivery; each UTF-8 part fits the transport's byte limit.
func splitMessage(text string, limit int) ([]string, error) {
	if !utf8.ValidString(text) {
		return nil, fmt.Errorf("message is not valid UTF-8")
	}
	if limit < utf8.UTFMax {
		return nil, fmt.Errorf("message limit must fit a UTF-8 character")
	}
	var parts []string
	for len(text) > limit {
		cut := limit
		for !utf8.RuneStart(text[cut]) {
			cut--
		}
		if newline := strings.LastIndexByte(text[:cut], '\n'); newline >= 0 {
			cut = newline + 1
		}
		parts = append(parts, text[:cut])
		text = text[cut:]
	}
	if text != "" || len(parts) == 0 {
		parts = append(parts, text)
	}
	return parts, nil
}
