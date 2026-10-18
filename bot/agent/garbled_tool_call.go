package agent

import "strings"

// maxGarbledLen bounds how long a leaked call may be. A real leak is a short
// wrapper around its arguments; longer content is an answer that quotes the
// syntax while discussing it (seen: a 12KB report quoting DSML wording).
const maxGarbledLen = 1500

// isGarbledToolCall recognizes leaked tool-call syntax in model text rather
// than an answer. Models occasionally emit harness-internal call syntax as
// message content instead of native tool calls; some tokenizers render the
// pipes as fullwidth variants (｜) or drop them, and some wrap the arguments
// in a "parameter" element without other markers.
//
// Detection is structural, not a bare keyword match: prose that mentions the
// syntax (a report on this codebase, docs, an explanation of the detector)
// must stay a normal answer. A leak always looks like a call — an opening
// tag with a name=, a closer, or a bare argument element — and it is never
// wrapped in markdown code, since markdown means the model is quoting.
func isGarbledToolCall(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	// A leaked call is a short wrapper around its arguments; long content is
	// an answer that merely quotes the syntax — unless it opens with a call
	// wrapper, which a report quoting the format never does. A leaked write
	// call can legitimately exceed the bound because of its arguments.
	if len(trimmed) > maxGarbledLen && !garbledPrefix(trimmed) {
		return false
	}
	// Fold fullwidth pipes so <｜｜DSML｜｜ calls> matches <|DSML calls>.
	body := strings.ReplaceAll(stripInlineCode(trimmed), "｜", "|")
	if garbledStructured(body) {
		return true
	}
	// The remainder is prose unless it quotes a full call outside code. Strip
	// inline code here as well: a report that shows a JSON call in backticks is
	// explaining the format, exactly like the structured-tag case above.
	return garbledJSONCall(body)
}

// Call wrappers. A leak is one call, not a sentence that names a few tags.
var garbledLaunchers = []string{
	"<invoke name=", "<tool_call>", "<tool_call ", "<tool_use>", "<tool_use ",
	"<parameter name=", "<parameter  name=",
	"<|DSML| invoke", "<||DSML|| invoke", "<DSML invoke", "<DSML function_calls",
}

// garbledPrefix reports text that opens with a call wrapper. A truncated
// stream carries only the start of the call, so it has no closing structure;
// an answer never begins with a tag.
func garbledPrefix(body string) bool {
	// Same code/pipe normalization as isGarbledToolCall: a truncated answer
	// that merely quotes a tag inline is still an answer.
	body = strings.ReplaceAll(strings.TrimSpace(stripInlineCode(body)), "｜", "|")
	lower := strings.ToLower(body)
	for _, marker := range garbledPrefixMarkers {
		if strings.HasPrefix(lower, marker) {
			return true
		}
	}
	return false
}

// DSML emits its own wrapper pair; a "calls" opener plus an invoke opener is
// the signature seen in the live failure, regardless of fullwidth pipes.
var dsmlOpeners = []string{"<|DSML| calls", "<||DSML|| calls", "<DSML calls"}

// garbledPrefixMarkers are the lowercase prefixes a leaked call can open with:
// the call wrappers plus the DSML "calls" wrapper that precedes an invoke.
var garbledPrefixMarkers = func() []string {
	markers := make([]string, 0, len(garbledLaunchers)+len(dsmlOpeners))
	for _, marker := range append(append([]string{}, garbledLaunchers...), dsmlOpeners...) {
		markers = append(markers, strings.ToLower(marker))
	}
	return markers
}()

var callContinuations = []string{
	"</invoke", "</tool_call", "</tool_use",
	"<parameter name=", "<parameter  name=",
	"<|DSML| parameter", "<||DSML|| parameter", "<DSML parameter",
}

func garbledStructured(body string) bool {
	lower := strings.ToLower(body)
	if !containsAny(lower, garbledLaunchers) {
		return false
	}
	if containsAny(lower, dsmlOpeners) {
		return true
	}
	// One wrapper plus the rest of the call structure. A sentence quoting a
	// launcher and a parameter keeps them in backticks, so stripping code
	// above already removed them.
	return containsAny(lower, callContinuations)
}

func containsAny(text string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(text, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}

// stripInlineCode excludes Markdown code spans and fenced blocks. Delimiters
// must match in length; a backtick inside a fenced example is ordinary text.
func stripInlineCode(body string) string {
	var b strings.Builder
	var fence byte
	fenceLen := 0
	for _, line := range strings.SplitAfter(body, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		if len(line)-len(trimmed) <= 3 && len(trimmed) > 0 && (trimmed[0] == '`' || trimmed[0] == '~') {
			n := 0
			for n < len(trimmed) && trimmed[n] == trimmed[0] {
				n++
			}
			if fence == 0 && n >= 3 {
				fence, fenceLen = trimmed[0], n
				continue
			}
			if fence != 0 && trimmed[0] == fence && n >= fenceLen && strings.TrimSpace(trimmed[n:]) == "" {
				fence = 0
				b.WriteByte('\n')
				continue
			}
		}
		if fence == 0 {
			b.WriteString(line)
		}
	}
	body = b.String()
	b.Reset()
	for i := 0; i < len(body); {
		if body[i] != '`' {
			b.WriteByte(body[i])
			i++
			continue
		}
		start := i
		for i < len(body) && body[i] == '`' {
			i++
		}
		width := i - start
		end := i
		matched := false
		for end < len(body) {
			if body[end] != '`' {
				end++
				continue
			}
			closeStart := end
			for end < len(body) && body[end] == '`' {
				end++
			}
			if end-closeStart == width {
				i = end
				matched = true
				break
			}
		}
		if !matched {
			b.WriteString(body[start:i])
		}
	}
	return b.String()
}

// garbledJSONCall reports a JSON-shaped tool call: a tool_calls/tool_use key
// together with either an arguments payload or a whole JSON document. A lone
// mention of the key in a sentence does not qualify.
func garbledJSONCall(body string) bool {
	hasKey := strings.Contains(body, `"tool_calls"`) || strings.Contains(body, `"tool_use"`)
	if !hasKey {
		return false
	}
	// The whole response is one JSON document: that is a call, not prose.
	braced := strings.HasPrefix(body, "{") || strings.HasPrefix(body, "[")
	if braced && (strings.HasSuffix(body, "}") || strings.HasSuffix(body, "]")) {
		return true
	}
	return strings.Contains(body, `"arguments"`) ||
		strings.Contains(body, `"name":"`) || strings.Contains(body, `"name": "`)
}
