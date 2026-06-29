package token

import "nekocode/bot/llm/types"

const asciiCharsPerToken = 4

// EstimateTokens uses a language-aware heuristic: ASCII ≈ 4 chars/token,
// CJK ≈ 1.5 chars/token. Used when API-calibrated counts are unavailable.
func EstimateTokens(msgs []types.Message) int {
	n := 0
	for _, m := range msgs {
		n += EstimateString(m.Role)
		n += EstimateString(m.Content)
		n += EstimateString(m.ReasoningContent)
		n += EstimateString(m.Name)
		for _, tc := range m.ToolCalls {
			n += EstimateString(tc.ID)
			n += EstimateString(tc.Function.Name)
			n += EstimateString(tc.Function.Arguments)
			n += 8
		}
	}
	return n
}

// EstimateString estimates token count for a single string.
func EstimateString(s string) int {
	if len(s) == 0 {
		return 0
	}
	asciiChars := 0
	cjkChars := 0
	for _, r := range s {
		if r <= 127 {
			asciiChars++
		} else if r >= 0x4E00 && r <= 0x9FFF || r >= 0x3040 && r <= 0x30FF || r >= 0xAC00 && r <= 0xD7AF {
			cjkChars++
		} else {
			asciiChars++
		}
	}
	tokens := (asciiChars + asciiCharsPerToken - 1) / asciiCharsPerToken
	tokens += (cjkChars*2 + 2) / 3
	return tokens
}
