package compact

import (
	"context"
	"fmt"
	"strings"

	"nekocode/bot/llm"
	"nekocode/bot/llm/types"
)

const (
	mergeMaxTokens = 2000
	mergeFailTag   = "[Merge Failed — raw append]"
)

// MergeSummaries runs an independent LLM session to merge old and new summaries.
// Uses a clean context (no history) with thinking disabled — fast and focused.
// Returns the merged summary, or falls back to raw append on failure.
func MergeSummaries(ctx context.Context, llmClient types.LLM, oldSummary, newSummary string) string {
	if oldSummary == "" {
		return newSummary
	}
	if newSummary == "" {
		return oldSummary
	}

	merged, err := tryMerge(ctx, llmClient, oldSummary, newSummary)
	if err != nil {
		// Fallback: raw string append with size limit to prevent unbounded growth.
		combined := oldSummary + "\n\n" + newSummary
		runes := []rune(combined)
		if len(runes) > mergeMaxTokens*4 {
			combined = string(runes[:mergeMaxTokens*4]) + "\n... (truncated)"
		}
		return fmt.Sprintf("%s\n\n---\n%s\n%s",
			combined, mergeFailTag,
			"Previous merge failed. Content preserved as-is. Async healing will clean up.")
	}
	return merged
}

func tryMerge(ctx context.Context, client types.LLM, oldSummary, newSummary string) (string, error) {
	// Save and restore both MaxTokens and DisableThinking to avoid
	// mutating shared state when concurrent sub-agents use the same client.
	origMaxTokens := client.GetMaxTokens()
	origThinking := client.GetDisableThinking()
	client.SetMaxTokens(mergeMaxTokens)
	client.SetDisableThinking(true)
	defer func() {
		client.SetMaxTokens(origMaxTokens)
		client.SetDisableThinking(origThinking)
	}()

	var merged string
	err := llm.Retry(ctx, llm.DefaultRetryConfig, func() error {
		m, err := callMerge(ctx, client, oldSummary, newSummary)
		if err != nil {
			return err
		}
		merged = m
		return nil
	})
	return merged, err
}

func callMerge(ctx context.Context, client types.LLM, oldSummary, newSummary string) (string, error) {
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
