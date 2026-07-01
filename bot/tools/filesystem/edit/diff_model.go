package edit

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
)

const structuredDiffMarker = "EDIT_PREVIEW_JSON_B64 "

type structuredDiff struct {
	Path  string               `json:"path"`
	Lines []structuredDiffLine `json:"lines"`
}

type structuredDiffLine struct {
	Kind   string `json:"kind"`
	LineNo int    `json:"line_no,omitempty"`
	Text   string `json:"text"`
}

func appendStructuredDiff(text, path string) string {
	model := structuredDiffFromText(text, path)
	if len(model.Lines) == 0 {
		return text
	}
	data, err := json.Marshal(model)
	if err != nil {
		return text
	}
	return strings.TrimRight(text, "\n") + "\n---\n" + structuredDiffMarker + base64.StdEncoding.EncodeToString(data)
}

// structuredDiffFromText parses a unified-diff-like text into a structured model.
// Supports two formats:
//   - New: "@@ -2,1 +2,1 @@\n-two\n+TWO\n"
//   - Old: " 1:one\n-2:two\n+2:TWO\n 3:three"
func structuredDiffFromText(text, path string) structuredDiff {
	model := structuredDiff{Path: path}
	for _, raw := range strings.Split(text, "\n") {
		// Unified diff hunk header
		if strings.HasPrefix(raw, "@@") {
			continue
		}
		// Edit file header [path#tag]
		if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") && strings.Contains(raw, "#") {
			if model.Path == "" {
				model.Path = pathFromHeader(raw)
			}
			continue
		}
		// Fold marker
		if strings.HasPrefix(raw, "…") || strings.HasPrefix(raw, "...") {
			model.Lines = append(model.Lines, structuredDiffLine{Kind: "fold", Text: raw})
			continue
		}
		// Separator
		if strings.TrimSpace(raw) == "---" {
			break
		}

		// Old format: "PREFIX:text" where PREFIX is " ", "+N", "-N", or "N"
		colon := strings.IndexByte(raw, ':')
		if colon > 0 && isNumberedDiffPrefix(raw[:colon]) {
			prefix := strings.TrimLeft(raw[:colon], " ")
			textPart := raw[colon+1:]
			kind := "ctx"
			lineNoText := prefix
			if strings.HasPrefix(prefix, "+") {
				kind = "add"
				lineNoText = strings.TrimPrefix(prefix, "+")
			} else if strings.HasPrefix(prefix, "-") {
				kind = "del"
				lineNoText = strings.TrimPrefix(prefix, "-")
			}
			lineNo, _ := strconv.Atoi(strings.TrimSpace(lineNoText))
			model.Lines = append(model.Lines, structuredDiffLine{
				Kind:   kind,
				LineNo: lineNo,
				Text:   textPart,
			})
			continue
		}

		// Unified diff lines: "+text" or "-text" or " text"
		if len(raw) > 0 {
			switch raw[0] {
			case '+':
				model.Lines = append(model.Lines, structuredDiffLine{Kind: "add", Text: raw[1:]})
			case '-':
				model.Lines = append(model.Lines, structuredDiffLine{Kind: "del", Text: raw[1:]})
			case ' ':
				model.Lines = append(model.Lines, structuredDiffLine{Kind: "ctx", Text: strings.TrimPrefix(raw, " ")})
			}
		}
	}
	return model
}

func isNumberedDiffPrefix(prefix string) bool {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return false
	}
	if prefix[0] == '+' || prefix[0] == '-' {
		prefix = prefix[1:]
	}
	if prefix == "" {
		return false
	}
	for _, r := range prefix {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func pathFromHeader(header string) string {
	tag := strings.TrimSuffix(strings.TrimPrefix(header, "["), "]")
	hashIdx := strings.LastIndexByte(tag, '#')
	if hashIdx <= 0 {
		return tag
	}
	return tag[:hashIdx]
}
