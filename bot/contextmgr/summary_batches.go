package contextmgr

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"nekocode/bot/contextmgr/token"
	"nekocode/bot/provider"
	"nekocode/bot/provider/types"
)

// Includes retries of empty provider responses, not only successful batches.
const maxSummaryCalls = 8

type summaryBatch struct {
	messages []types.Message
	archive  string
}

func (s *replacementCompactor) summaryWindow() int {
	if s.modelWindow > 0 {
		return s.modelWindow
	}
	return defaultBudget
}

func (s *replacementCompactor) summaryOutput() int {
	if output := provider.MaxOutputTokensFor(s.model); output > 0 {
		return output
	}
	return min(4096, max(1, s.summaryWindow()/4))
}

func (s *replacementCompactor) summarizeBatches(messages []types.Message, archive string, summarize Summarizer) (string, error) {
	batches, err := s.planSummaryBatches(messages, archive)
	if err != nil {
		return "", err
	}
	previous := ""
	for i, batch := range batches {
		if i == 0 {
			previous = batch.archive
		}
		if _, err := prepareSummaryRequest(batch.messages, previous, s.summaryWindow(), s.model); err != nil {
			return "", err
		}
		raw, err := summarize(batch.messages, previous)
		if err != nil {
			return "", fmt.Errorf("summary batch %d/%d: %w", i+1, len(batches), err)
		}
		previous = extractXMLBlock(raw, "summary")
		if previous == "" {
			return "", fmt.Errorf("summary batch %d/%d: missing complete non-empty <summary> block; history preserved", i+1, len(batches))
		}
		if token.EstimateString(previous) > s.summaryOutput() {
			return "", fmt.Errorf("summary batch %d/%d exceeded output reserve; history preserved", i+1, len(batches))
		}
	}
	return previous, nil
}

// Plan every source fragment before calling a model. Reserve the maximum
// intermediate summary on every batch, independent of what the model returns.
func (s *replacementCompactor) planSummaryBatches(messages []types.Message, archive string) ([]summaryBatch, error) {
	_, originalBudgetError := prepareSummaryRequest(messages, archive, s.summaryWindow(), s.model)
	if originalBudgetError == nil {
		return []summaryBatch{{messages: messages, archive: archive}}, nil
	}
	fits := func(batch []types.Message) bool {
		request := buildSummaryMessages(batch, "")
		budget, _ := estimateRequestBudget(request, nil, s.summaryWindow(), s.summaryOutput(), provider.ReasoningSettingsFor(s.model), false, 0)
		// Previous archive heading and JSON/framing slack are separate from body.
		return budget.Total()+s.summaryOutput()+32 <= budget.Window
	}
	var sources []types.Message
	if archive != "" {
		sources = append(sources, types.Message{Role: "archive", Content: archive})
	}
	sources = append(sources, messages...)
	var batches []summaryBatch
	var current []types.Message
	flush := func() {
		if len(current) > 0 {
			batches = append(batches, summaryBatch{messages: current})
			current = nil
		}
	}
	for _, source := range sources {
		if fits(append(append([]types.Message(nil), current...), source)) {
			current = append(current, source)
			continue
		}
		flush()
		if fits([]types.Message{source}) {
			current = []types.Message{source}
			continue
		}
		// Split the full serialized record, including giant tool arguments, on rune
		// boundaries. Content hashes remain stable when active history indices move.
		data := formatSummaryMessages([]types.Message{source})
		id := fmt.Sprintf("%x", sha256.Sum256([]byte(data)))
		runes := []rune(data)
		offset, part := 0, 1
		for offset < len(runes) {
			fragment := func(end int) types.Message {
				body, _ := json.Marshal(struct {
					Source string `json:"source_sha256"`
					Part   int    `json:"part"`
					Final  bool   `json:"final"`
					Data   string `json:"data"`
				}{id, part, end == len(runes), string(runes[offset:end])})
				return types.Message{Role: "user", Content: string(body)}
			}
			low, high := offset, len(runes)
			for low < high {
				mid := low + (high-low+1)/2
				if fits([]types.Message{fragment(mid)}) {
					low = mid
				} else {
					high = mid - 1
				}
			}
			if low == offset {
				return nil, fmt.Errorf("summary window %d cannot fit a source fragment and reserved summary; choose a larger compaction window: %w", s.summaryWindow(), originalBudgetError)
			}
			batches = append(batches, summaryBatch{messages: []types.Message{fragment(low)}})
			if len(batches) > maxSummaryCalls {
				return nil, s.summaryBatchLimitError()
			}
			offset, part = low, part+1
		}
	}
	flush()
	if len(batches) > maxSummaryCalls {
		return nil, s.summaryBatchLimitError()
	}
	return batches, nil
}

func (s *replacementCompactor) summaryBatchLimitError() error {
	return fmt.Errorf("summary input exceeds %d batches with window %d; history preserved; choose a larger compaction window", maxSummaryCalls, s.summaryWindow())
}
