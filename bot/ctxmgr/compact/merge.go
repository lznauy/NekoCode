package compact

import (
	"context"
	"fmt"
	"strings"
	"time"

	"nekocode/llm/types"
)

const (
	mergeMaxTokens   = 2000
	mergeMaxRetries  = 3
	mergeBaseBackoff = 500 * time.Millisecond
	mergeFailTag     = "[Merge Failed — raw append]"
)

// MergeSummaries runs an independent LLM session to merge old and new summaries.
// Uses a clean context (no history) with thinking disabled — fast and focused.
// Returns the merged summary, or falls back to raw append on failure.
func MergeSummaries(llmClient types.LLM, oldSummary, newSummary string) string {
	if oldSummary == "" {
		return newSummary
	}
	if newSummary == "" {
		return oldSummary
	}

	merged, err := tryMerge(llmClient, oldSummary, newSummary)
	if err != nil {
		// Fallback: raw string append with failure tag.
		return fmt.Sprintf("%s\n\n%s\n\n---\n%s\n%s",
			oldSummary, newSummary, mergeFailTag,
			"Previous merge failed. Content preserved as-is. Async healing will clean up.")
	}
	return merged
}

func tryMerge(client types.LLM, oldSummary, newSummary string) (string, error) {
	client.SetMaxTokens(mergeMaxTokens)
	client.SetDisableThinking(true)

	var lastErr error
	for attempt := 0; attempt < mergeMaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(mergeBaseBackoff * time.Duration(1<<uint(attempt-1)))
		}

		merged, err := callMerge(client, oldSummary, newSummary)
		if err == nil {
			return merged, nil
		}
		lastErr = err
	}
	return "", fmt.Errorf("merge failed after %d retries: %w", mergeMaxRetries, lastErr)
}

func callMerge(client types.LLM, oldSummary, newSummary string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	prompt := buildMergePrompt(oldSummary, newSummary)
	resp, err := client.Chat(ctx, []types.Message{{Role: "user", Content: prompt}}, nil)
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("empty merge response")
	}
	text := strings.TrimSpace(resp.Choices[0].Message.Content)
	if text == "" {
		return "", fmt.Errorf("empty merge content")
	}
	return text, nil
}

func buildMergePrompt(oldSummary, newSummary string) string {
	return fmt.Sprintf(`Merge the following two exploration summaries into one concise, deduplicated summary.
Keep ONLY the latest information for each module. Remove contradictions by trusting the newer summary.

Rules:
- Same module path → keep the NEWER State and Main_Responsibility
- If a Key_Dependency appears in both, merge (union)
- If information conflicts, trust the NEWER
- Output ONLY the merged summaries in the same format — no commentary.

OLD SUMMARY:
%s

NEW SUMMARY:
%s

MERGED:`, oldSummary, newSummary)
}
