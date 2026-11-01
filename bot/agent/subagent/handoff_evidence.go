package subagent

import (
	"fmt"
	"strings"

	"nekocode/bot/extension/tool/runtime/refbridge"
)

func optionalEvidenceIDs(args map[string]any) ([]string, error) {
	raw, exists := args["evidence_ids"]
	if !exists {
		return nil, nil
	}
	if values, ok := raw.([]any); ok && len(values) > MaxEvidenceIDs {
		return nil, fmt.Errorf("evidence_ids allows at most %d references", MaxEvidenceIDs)
	}
	ids, err := requiredResultStrings(args, "evidence_ids")
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if len(id) > 128 || !strings.HasPrefix(id, "evidence.") {
			return nil, fmt.Errorf("evidence_ids requires evidence references")
		}
		if _, err := refbridge.Sequence(id); err != nil {
			return nil, fmt.Errorf("invalid evidence reference")
		}
		if seen[id] {
			return nil, fmt.Errorf("duplicate evidence reference")
		}
		seen[id] = true
	}
	return ids, nil
}
