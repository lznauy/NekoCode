package editdsl

import (
	"fmt"
	"regexp"
	"strings"
)

// applyPatchPathNoiseRe matches apply_patch-style path noise prefixes.
var applyPatchPathNoiseRe = regexp.MustCompile(`^\*{0,3}\s*(?:(?:[Uu]pdate|[Aa]dd|[Dd]elete|[Mm]ove)[^A-Za-z0-9]*(?:[Ff]ile|[Tt]o)?[^A-Za-z0-9]*:)?\s*\*{0,3}\s*`)

// stripApplyPatchNoise removes apply_patch-style path noise from file headers.
func stripApplyPatchNoise(path string) string {
	return strings.TrimSpace(applyPatchPathNoiseRe.ReplaceAllString(path, ""))
}

// parsePayload reads +TEXT lines until the next hunk header, file header, or end marker.
// Bare body rows are auto-prefixed with a sentinel so ApplyEdits can warn.
func parsePayload(lines []string, i int) ([]string, int, error) {
	var literal []payloadRow
	var deferred []payloadRow

	commitBlanks := func() {
		literal = append(literal, deferred...)
		deferred = nil
	}

	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if trimmed == "*** Abort" || trimmed == "*** End Patch" ||
			isHeaderLine(trimmed) ||
			strings.HasPrefix(trimmed, "replace ") ||
			strings.HasPrefix(trimmed, "delete ") ||
			strings.HasPrefix(trimmed, "insert ") {
			break
		}

		if strings.HasPrefix(trimmed, "+") {
			commitBlanks()
			literal = append(literal, payloadRow{text: trimmed[1:], bare: false})
			i++
			continue
		}

		if trimmed == "" {
			if len(literal) > 0 || len(deferred) > 0 {
				deferred = append(deferred, payloadRow{text: "", bare: true})
			}
			i++
			continue
		}

		if cont := detectApplyPatchContamination(trimmed); cont != nil {
			return nil, i, fmt.Errorf("line %d: %s", i+1, *cont)
		}

		if strings.HasPrefix(trimmed, "-") {
			return nil, i, fmt.Errorf("line %d: '-' rows are not valid. "+
				"The range already names the lines being changed. "+
				"For a literal '-' line, write '+-...'.", i+1)
		}

		commitBlanks()
		literal = append(literal, payloadRow{text: strings.TrimRight(line, " \t\r"), bare: true})
		i++
	}

	stripBarePrefixesIfUniform(literal)

	all := append(literal, deferred...)
	n := len(all)
	for n > 0 && all[n-1].bare && all[n-1].text == "" {
		n--
	}
	payload := make([]string, 0, n)
	for j := 0; j < n; j++ {
		if all[j].bare {
			payload = append(payload, autoprefixSentinel+all[j].text)
		} else {
			payload = append(payload, all[j].text)
		}
	}
	return payload, i, nil
}

// hlPrefixRe matches the read-output line-number prefix (e.g. "42:", ">>>42:", "+42:").
var hlPrefixRe = regexp.MustCompile(`^\s*(?:>>>|>>)?\s*(?:[+*-]\s*)?\d+:`)

func stripOneLeadingHashlinePrefix(line string) string {
	return hlPrefixRe.ReplaceAllString(line, "")
}

// bareLiteralValueRe matches lone quoted/string literals or numeric values,
// optionally comma-terminated; those are content, not read-output paste.
var bareLiteralValueRe = regexp.MustCompile(`^\s*(?:"[^"]*"|'[^']*'|[-+]?\d+(?:\.\d+)?)\s*,?\s*$`)

type payloadRow = struct {
	text string
	bare bool
}

// stripBarePrefixesIfUniform strips a single read-output line-number prefix
// from every bare body row, but only when all bare rows carry one uniformly.
func stripBarePrefixesIfUniform(rows []payloadRow) {
	r := rows
	var bareIdxs []int
	for i := range r {
		if r[i].bare && r[i].text != "" {
			bareIdxs = append(bareIdxs, i)
		}
	}
	if len(bareIdxs) < 1 {
		return
	}
	for _, i := range bareIdxs {
		stripped := stripOneLeadingHashlinePrefix(r[i].text)
		if stripped == r[i].text {
			return
		}
	}
	allLiterals := true
	for _, i := range bareIdxs {
		stripped := stripOneLeadingHashlinePrefix(r[i].text)
		if !bareLiteralValueRe.MatchString(stripped) {
			allLiterals = false
			break
		}
	}
	if allLiterals {
		return
	}
	for _, i := range bareIdxs {
		r[i].text = stripOneLeadingHashlinePrefix(r[i].text)
	}
}
