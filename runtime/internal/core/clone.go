package core

import (
	"encoding/json"
	"maps"
	"slices"
	"time"

	"nekocode/protocol"
)

func CloneMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = CloneJSONValue(value)
	}
	return output
}

func CloneJSONValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return CloneMap(value)
	case []any:
		output := make([]any, len(value))
		for i := range value {
			output[i] = CloneJSONValue(value[i])
		}
		return output
	case map[string]string:
		return maps.Clone(value)
	case json.RawMessage:
		return slices.Clone(value)
	case []byte:
		return slices.Clone(value)
	case []string:
		return append([]string(nil), value...)
	default:
		return value
	}
}

func CloneQuestions(input []protocol.QuestionItem) []protocol.QuestionItem {
	output := append([]protocol.QuestionItem(nil), input...)
	for i := range output {
		output[i].Options = append([]protocol.QuestionOption(nil), input[i].Options...)
	}
	return output
}

func (v ApprovalView) Clone() ApprovalView {
	v.Args = CloneMap(v.Args)
	v.Metadata = CloneMap(v.Metadata)
	v.Approval = v.Approval.Clone()
	v.ResolvedAt = cloneResolvedAt(v.ResolvedAt)
	return v
}

func (v QuestionView) Clone() QuestionView {
	v.Questions = CloneQuestions(v.Questions)
	v.ResolvedAt = cloneResolvedAt(v.ResolvedAt)
	return v
}

func cloneResolvedAt(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	copied := *t
	return &copied
}
