package textutil

import (
	"regexp"

	"nekocode/bot/tools/editcore"
)

var ansiRegex = regexp.MustCompile("\x1b\\[[0-9;]*[a-zA-Z]")

// StripAnsi removes ANSI escape sequences from a string.
func StripAnsi(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

// NormalizeText strips ANSI escapes and normalizes line endings to LF.
func NormalizeText(text string) string {
	text = StripAnsi(text)
	return editcore.NormalizeToLF(text)
}
