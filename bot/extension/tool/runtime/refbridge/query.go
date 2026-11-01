package refbridge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	toolcore "nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/workspace"
	"nekocode/bot/provider/types"
	"nekocode/protocol"
	textutil "nekocode/util/text"
)

// Read assumes its caller has bound this source, scope and tail to one actor.
// Source failures are redacted; inspection never grants read or write access.
func (s Source) Read(ctx context.Context, sessionID string, q Query, base int, tail []types.Message) (Page, error) {
	if err := ctx.Err(); err != nil {
		return Page{}, err
	}
	if q.TaskID != "" || q.AttemptID != "" {
		return Page{}, fmt.Errorf("explicit attempt requires parent binding")
	}
	if sessionID == "" || base < 0 || q.Before < 0 || q.Limit < 1 || q.Limit > MaxPageRecords {
		return Page{}, fmt.Errorf("invalid reference page")
	}
	if q.Kind == EvidenceKind {
		return s.readEvidence(ctx, sessionID, q, base, tail, 1, protocol.ExecutionFromContext(ctx), false)
	}
	if q.Kind != "" {
		return Page{}, fmt.Errorf("invalid reference kind")
	}
	out := Page{SessionID: sessionID, Items: []Reference{}}
	if q.ID == "" {
		page, err := s.referencePage(ctx, base, tail, q.Before, q.Limit, 64*1024)
		if cancelErr := ctx.Err(); cancelErr != nil {
			return out, cancelErr
		}
		if err != nil {
			return out, fmt.Errorf("reference history unavailable")
		}
		out.Before, out.HasMore, out.Scanned = page.Before, page.HasMore, len(page.Records)
		for _, record := range page.Records {
			msg := record.Message
			if msg.Role == "tool" && msg.ToolOutput.Path != "" {
				out.Items = append(out.Items, ArtifactReference(sessionID, record.Sequence, msg.ToolOutput))
			}
			if ref, ok := SkillReference(sessionID, record.Sequence, msg); ok {
				out.Items = append(out.Items, ref)
			}
		}
		return out, nil
	}
	sequence, err := Sequence(q.ID)
	if err != nil {
		return out, err
	}
	msg, found, err := s.record(ctx, sequence, base, tail)
	if cancelErr := ctx.Err(); cancelErr != nil {
		return out, cancelErr
	}
	if err != nil {
		return out, fmt.Errorf("reference history unavailable")
	}
	if found {
		out.Scanned = 1
		out.Before = sequence
		out.HasMore = sequence > 1
	}
	out.Status = "missing"
	if !found {
		return out, nil
	}
	if strings.HasPrefix(q.ID, "skill.") {
		ref, ok := SkillReference(sessionID, sequence, msg)
		out.Status = "changed"
		if !ok || ref.ID != q.ID {
			return out, nil
		}
		out.Items = []Reference{ref}
		out.Status = "unavailable"
		if s.Skill == nil || toolcore.OutputStoreFromContext(ctx) == nil {
			return out, nil
		}
		current, exists := s.Skill(ref.Name)
		if !exists {
			out.Status = "missing"
			return out, nil
		}
		currentText := current
		if ref.Source == "tool" {
			currentText = textutil.NormalizeTerminalOutput(currentText)
		}
		if types.SkillContentHash(currentText) != ref.ContentHash {
			out.Status = "changed"
			return out, nil
		}
		out.Status = "available"
		return out, nil
	}
	artifact := msg.ToolOutput
	if msg.Role != "tool" || artifact.Path == "" {
		out.Status = "changed"
		return out, nil
	}
	ref := ArtifactReference(sessionID, sequence, artifact)
	if ref.ID != q.ID {
		out.Status = "changed"
		return out, nil
	}
	out.Items = []Reference{ref}
	out.Status = "unavailable"
	manager, ok := workspace.FromContext(ctx)
	store := toolcore.OutputStoreFromContext(ctx)
	if !ok || store == nil {
		return out, nil
	}
	_, _, allowed, err := manager.CheckRead(artifact.Path)
	if err != nil || !allowed {
		return out, nil
	}
	// Existence details are returned only inside the already authorized scope.
	if _, err := os.Lstat(artifact.Path); errors.Is(err, os.ErrNotExist) {
		out.Status = "missing"
		return out, nil
	}
	if err := store.CheckArtifact(artifact); err != nil {
		return out, nil
	}
	out.Status, out.Path = "available", artifact.Path
	return out, nil
}

func (s Source) record(ctx context.Context, sequence, base int, tail []types.Message) (types.Message, bool, error) {
	if sequence > base {
		if sequence-base > len(tail) {
			return types.Message{}, false, nil
		}
		return tail[sequence-base-1], true, nil
	}
	if s.Record == nil {
		return types.Message{}, false, fmt.Errorf("reference history unavailable")
	}
	return s.Record(ctx, sequence, base)
}
