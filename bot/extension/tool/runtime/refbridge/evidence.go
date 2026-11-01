package refbridge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

// readEvidence scans a bounded raw page, independently of artifact/skill links.
// No extra index or full-history search is required to inspect a returned ID.
func (s Source) readEvidence(ctx context.Context, scope string, q Query, base int, tail []types.Message, first int, owner protocol.ExecutionIdentity, exact bool) (Page, error) {
	out := Page{SessionID: scope, Items: []Reference{}, Evidence: []EvidenceItem{}}
	if core.OutputStoreFromContext(ctx) == nil {
		return out, fmt.Errorf("reference actor unavailable")
	}
	end := base + len(tail)
	if q.Before > 0 {
		end = min(end, q.Before-1)
	}
	if q.ID != "" {
		if !strings.HasPrefix(q.ID, "evidence.") {
			return out, fmt.Errorf("invalid evidence ID")
		}
		inspectSequence, err := Sequence(q.ID)
		if err != nil {
			return out, err
		}
		if inspectSequence > end || inspectSequence < first {
			out.Status = "missing"
			if exact {
				out.Status = "wrong_scope"
			}
			return out, nil
		}
		end = inspectSequence
	}
	bytes := 0
	// Reserve room for both physical invocations before consuming a source record.
	for sequence := end; sequence >= first && out.Scanned < q.Limit && (q.ID != "" || len(out.Evidence) <= MaxPageRecords-2); sequence-- {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		msg, found, err := s.record(ctx, sequence, base, tail)
		if cancelErr := ctx.Err(); cancelErr != nil {
			return out, cancelErr
		}
		if err != nil || !found {
			return out, fmt.Errorf("evidence history unavailable")
		}
		out.Scanned++
		// Count decoded candidates, including one deferred at the byte boundary.
		data, _ := json.Marshal(msg)
		if out.Scanned > 1 && bytes+len(data) > 64*1024 {
			break
		}
		bytes += len(data)
		out.Before = sequence
		out.HasMore = sequence > first
		if q.ID != "" {
			out.Status = "missing"
		}
		if msg.Role == "tool" && msg.ToolEvidence.Schema == 1 && len(msg.ToolEvidence.Executions) <= 2 {
			for _, receipt := range msg.ToolEvidence.Executions {
				item := evidenceItem(scope, sequence, msg.ToolEvidence, receipt)
				if !ownedReceipt(receipt, owner, exact) {
					if exact && q.ID == item.ID {
						out.Status = "wrong_scope"
					}
					continue
				}
				if q.ID != "" {
					if item.ID != q.ID {
						continue
					}
					detail := msg.ToolEvidence
					receipt.Artifact = protocol.ToolOutputArtifact{Incomplete: receipt.Artifact.Incomplete}
					detail.Executions = []protocol.ExecutionReceipt{receipt}
					item.Detail = &detail
					out.Evidence = []EvidenceItem{item}
					out.Status = "recorded"
					return out, nil
				}
				out.Evidence = append(out.Evidence, item)
			}
		}
		if q.ID != "" {
			return out, nil
		}
	}
	return out, nil
}

func ownedReceipt(receipt protocol.ExecutionReceipt, actor protocol.ExecutionIdentity, exact bool) bool {
	id, err := uuid.Parse(receipt.ID)
	if err != nil || id.String() != receipt.ID || receipt.Tool == "" {
		return false
	}
	if exact && receipt.Execution.AttemptID != actor.AttemptID {
		return false
	}
	// Own-task history includes prior attempts; selecting another task or parent
	// is impossible. The outer reader still checks the current attempt/store.
	if actor.TaskID != "" {
		return receipt.Execution.TaskID == actor.TaskID && receipt.Execution.AttemptID != ""
	}
	return receipt.Execution.TaskID == "" && receipt.Execution.AttemptID == ""
}

func evidenceItem(scope string, sequence int, evidence protocol.ToolEvidence, receipt protocol.ExecutionReceipt) EvidenceItem {
	// Bind the alias to both scope and the actual stored receipt/delivery values.
	evidence.Executions = []protocol.ExecutionReceipt{receipt}
	data, _ := json.Marshal(struct {
		Scope    string
		Sequence int
		Evidence protocol.ToolEvidence
	}{scope, sequence, evidence})
	digest := sha256.Sum256(data)
	return EvidenceItem{ID: fmt.Sprintf("evidence.%d.%s", sequence, hex.EncodeToString(digest[:])), Sequence: sequence, ReceiptID: receipt.ID, Tool: receipt.Tool, Status: receipt.Status, HasArtifact: receipt.Artifact.Path != ""}
}

// ReadAttemptEvidence is for a parent-bound source whose owner and committed
// sequence interval were established from the authoritative child metadata.
// It does not impersonate the child actor in ctx or grant file permissions.
func (s Source) ReadAttemptEvidence(ctx context.Context, scope string, q Query, first, last int, owner protocol.ExecutionIdentity) (Page, error) {
	if err := ctx.Err(); err != nil {
		return Page{}, err
	}
	if scope == "" || q.Kind != EvidenceKind || q.TaskID != owner.TaskID || q.AttemptID != owner.AttemptID || owner.TaskID == "" || owner.AttemptID == "" || first < 1 || last < first-1 || q.Before < 0 || q.Limit < 1 || q.Limit > MaxPageRecords {
		return Page{}, fmt.Errorf("invalid attempt evidence query")
	}
	return s.readEvidence(ctx, scope, q, last, nil, first, owner, true)
}
