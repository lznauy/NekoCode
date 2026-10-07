package token

import "nekocode/bot/provider/types"

const asciiCharsPerToken = 4

// EstimateTokens uses a language-aware heuristic: ASCII ≈ 4 chars/token,
// CJK ≈ 1.5 chars/token. Used when API-calibrated counts are unavailable.
// Image tokens are always included: callers without a model context should
// stay on the conservative (higher) side.
func EstimateTokens(msgs []types.Message) int {
	return estimateTokens(msgs, nil, true)
}

// EstimateModelTokens estimates the messages as serialized for the active
// model, excluding locally retained reasoning that its replay contract omits.
// Image tokens are charged only when the model receives native image input.
func EstimateModelTokens(msgs []types.Message, reasoning types.ReasoningSettings, vision bool) int {
	return estimateTokens(msgs, &reasoning, vision)
}

func estimateTokens(msgs []types.Message, reasoning *types.ReasoningSettings, vision bool) int {
	n := 0
	for _, m := range msgs {
		n += EstimateString(m.Role)
		n += EstimateString(m.Content)
		if vision {
			for _, image := range m.Images {
				n += EstimateImageTokens(image)
			}
		}
		if reasoning == nil {
			n += EstimateString(m.ReasoningContent)
			n += EstimateString(m.ReasoningSignature)
		} else if content, replay := types.ReasoningForRequest(m, *reasoning); replay {
			n += EstimateString(content)
			n += EstimateString(m.ReasoningSignature)
		}
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

// EstimateImageTokens approximates one image's visual-token cost using the
// Anthropic formula (width*height/750), which is also a reasonable proxy for
// OpenAI/Gemini tile pricing. Unknown dimensions fall back to the storage
// compression cap (1568x1568, mirroring util/attachment's storageMaxDimension),
// the largest image native input can carry.
func EstimateImageTokens(image types.MessageImage) int {
	if image.Width <= 0 || image.Height <= 0 {
		return (storageMaxImageDimension * storageMaxImageDimension) / 750
	}
	return (image.Width * image.Height) / 750
}

const storageMaxImageDimension = 1568
