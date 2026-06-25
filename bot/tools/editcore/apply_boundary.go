package editcore

import (
	"fmt"
	"strings"
)

func repairBoundaries(allLines []string, h Hunk, payload []string) ([]string, []string) {
	if len(payload) < 2 {
		return payload, nil
	}
	start := h.Start - 1
	end := h.End

	leadCount := 0
	for leadCount < len(payload) && start-1-leadCount >= 0 {
		pTrimmed := strings.TrimSpace(payload[leadCount])
		if pTrimmed == "" || pTrimmed != strings.TrimSpace(allLines[start-1-leadCount]) {
			break
		}
		leadCount++
	}

	trailCount := 0
	for trailCount < len(payload) && end+trailCount < len(allLines) {
		idx := len(payload) - 1 - trailCount
		pTrimmed := strings.TrimSpace(payload[idx])
		if pTrimmed == "" || pTrimmed != strings.TrimSpace(allLines[end+trailCount]) {
			break
		}
		trailCount++
	}
	if leadCount+trailCount >= len(payload) {
		return payload, nil
	}

	var warnings []string
	if leadCount > 0 {
		stripped := payload[:leadCount]
		payload = payload[leadCount:]
		msg := fmt.Sprintf(
			"BOUNDARY REPAIR at replace %d..%d: stripped %d leading payload line(s) "+
				"that already exist above the range.",
			h.Start, h.End, leadCount)
		msg += "\n  The stripped line(s):"
		for _, line := range stripped {
			msg += fmt.Sprintf("\n    %q", line)
		}
		msg += fmt.Sprintf(
			"\n  Your replace range (%d..%d) may be too narrow — these lines belong inside the "+
				"range, not outside it. Widen the range to include them, or use replace block %d "+
				"to auto-detect the construct boundary instead of counting lines manually.",
			h.Start, h.End, h.Start)
		warnings = append(warnings, msg)
	}
	if trailCount > 0 {
		stripped := payload[len(payload)-trailCount:]
		payload = payload[:len(payload)-trailCount]

		hasStructural := false
		for _, line := range stripped {
			if isStructuralCloser(line) {
				hasStructural = true
				break
			}
		}

		msg := fmt.Sprintf(
			"BOUNDARY REPAIR at replace %d..%d: stripped %d trailing payload line(s) "+
				"that already exist below the range.",
			h.Start, h.End, trailCount)
		msg += "\n  The stripped line(s):"
		for _, line := range stripped {
			msg += fmt.Sprintf("\n    %q", line)
		}
		if hasStructural {
			msg += fmt.Sprintf(
				"\n  The stripped lines include structural closers (} ] )). "+
					"Your range is too narrow — it ends before the block's closing delimiter. "+
					"Use replace block %d instead of replace %d..%d to let the tool detect "+
					"the full construct boundary automatically.",
				h.Start, h.Start, h.End)
		} else {
			msg += fmt.Sprintf(
				"\n  Your replace range (%d..%d) may end too early — these lines belong inside "+
					"the range, not below it. Widen the range to include them.",
				h.Start, h.End)
		}
		warnings = append(warnings, msg)
	}
	return payload, warnings
}
