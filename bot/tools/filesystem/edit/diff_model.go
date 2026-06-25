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

func structuredDiffFromText(text, path string) structuredDiff {
	model := structuredDiff{Path: path}
	for _, raw := range strings.Split(text, "\n") {
		if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") && strings.Contains(raw, "#") {
			if model.Path == "" {
				model.Path = pathFromHeader(raw)
			}
			continue
		}
		if strings.HasPrefix(raw, "…") {
			model.Lines = append(model.Lines, structuredDiffLine{Kind: "fold", Text: raw})
			continue
		}
		if strings.TrimSpace(raw) == "---" {
			break
		}
		colon := strings.IndexByte(raw, ':')
		if colon <= 0 {
			continue
		}
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
	}
	return model
}

func pathFromHeader(header string) string {
	tag := strings.TrimSuffix(strings.TrimPrefix(header, "["), "]")
	hashIdx := strings.LastIndexByte(tag, '#')
	if hashIdx <= 0 {
		return tag
	}
	return tag[:hashIdx]
}
