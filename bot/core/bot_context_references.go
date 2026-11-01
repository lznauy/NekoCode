package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	toolcore "nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/refbridge"
	"nekocode/bot/extension/tool/runtime/workspace"
	"nekocode/bot/provider/types"
	"nekocode/bot/session"
	"nekocode/protocol"
	textutil "nekocode/util/text"
)

func artifactReference(sessionID string, sequence int, artifact protocol.ToolOutputArtifact) refbridge.Reference {
	// Hash the source location and scope, not the current file contents. This ID
	// survives compaction and never pretends to be a content-version receipt.
	data, _ := json.Marshal(struct {
		Session  string
		Sequence int
		Artifact protocol.ToolOutputArtifact
	}{sessionID, sequence, artifact})
	digest := sha256.Sum256(data)
	return refbridge.Reference{Kind: "artifact", ID: fmt.Sprintf("artifact.%d.%s", sequence, hex.EncodeToString(digest[:])), Sequence: sequence, Incomplete: artifact.Incomplete}
}

func referenceSequence(id string) (int, error) {
	parts := strings.Split(id, ".")
	if len(parts) != 3 || (parts[0] != "artifact" && parts[0] != "skill") || len(parts[2]) != 64 {
		return 0, fmt.Errorf("invalid reference ID")
	}
	if _, err := hex.DecodeString(parts[2]); err != nil {
		return 0, fmt.Errorf("invalid reference ID")
	}
	seq, err := strconv.Atoi(parts[1])
	if err != nil || seq < 1 || strconv.Itoa(seq) != parts[1] || seq == int(^uint(0)>>1) {
		return 0, fmt.Errorf("invalid reference ID")
	}
	return seq, nil
}

// rawHistory drops the replay-only assistant header used by display pages.
// The remaining records map exactly to inclusive [Before, End] sequences.
func rawHistory(page session.HistoryPage) ([]types.Message, error) {
	count := max(0, page.End-page.Before+1)
	if page.End == 0 {
		count = 0
	}
	if count > len(page.Messages) {
		return nil, fmt.Errorf("invalid history reference page")
	}
	return page.Messages[len(page.Messages)-count:], nil
}

func (b *Bot) readContextReferences(ctx context.Context, q refbridge.Query) (refbridge.Page, error) {
	if toolcore.OutputStoreFromContext(ctx) == nil {
		return refbridge.Page{}, fmt.Errorf("reference actor unavailable")
	}
	return b.contextReferences(ctx, b.sess.CurrentID(), q)
}

// contextReferences binds history, cursor and scope under the existing history
// lock. No new reference index or file permission is created.
func (b *Bot) contextReferences(ctx context.Context, sessionID string, q refbridge.Query) (refbridge.Page, error) {
	if err := ctx.Err(); err != nil {
		return refbridge.Page{}, err
	}
	if protocol.ExecutionFromContext(ctx).TaskID != "" {
		return refbridge.Page{}, fmt.Errorf("context_refs is limited to the main session actor")
	}
	if q.Before < 0 || q.Limit < 1 || q.Limit > refbridge.MaxPageRecords {
		return refbridge.Page{}, fmt.Errorf("invalid reference page")
	}
	b.historyMu.RLock()
	defer b.historyMu.RUnlock()
	if sessionID == "" || b.sess.CurrentID() != sessionID {
		return refbridge.Page{}, fmt.Errorf("reference scope unavailable")
	}
	// Output ownership also rejects legacy/standalone children with no TaskID.
	if store := toolcore.OutputStoreFromContext(ctx); store != nil && store != b.sessionOutput.store {
		return refbridge.Page{}, fmt.Errorf("reference actor unavailable")
	}
	out := refbridge.Page{SessionID: sessionID, Items: []refbridge.Reference{}}
	before, limit := q.Before, q.Limit
	sequence := 0
	if q.ID != "" {
		var err error
		sequence, err = referenceSequence(q.ID)
		if err != nil {
			return out, err
		}
		before, limit = sequence+1, 1
	}
	if q.ID == "" {
		page, err := b.referenceHistoryLocked(ctx, before, limit, 64*1024)
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
				out.Items = append(out.Items, artifactReference(sessionID, record.Sequence, msg.ToolOutput))
			}
			if ref, ok := skillReference(sessionID, record.Sequence, msg); ok {
				out.Items = append(out.Items, ref)
			}
		}
		return out, nil
	}
	history, err := b.historyPageLocked(before, limit, 64*1024)
	if err != nil {
		return out, fmt.Errorf("reference history unavailable")
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	records, err := rawHistory(history)
	if err != nil {
		return out, err
	}
	out.Before, out.HasMore, out.Scanned = history.Before, history.HasMore, len(records)
	if q.ID != "" {
		out.Status = "missing"
		if history.End != sequence || len(records) != 1 {
			return out, nil
		}
		if strings.HasPrefix(q.ID, "skill.") {
			ref, ok := skillReference(sessionID, sequence, records[0])
			out.Status = "changed"
			if !ok || ref.ID != q.ID {
				return out, nil
			}
			out.Items = []refbridge.Reference{ref}
			out.Status = "unavailable"
			if b.ext == nil || toolcore.OutputStoreFromContext(ctx) == nil {
				return out, nil
			}
			current, exists := b.ext.Skill(ref.Name)
			if !exists {
				out.Status = "missing"
				return out, nil
			}
			currentText := current.Context
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
		artifact := records[0].ToolOutput
		if records[0].Role != "tool" || artifact.Path == "" {
			out.Status = "changed"
			return out, nil
		}
		ref := artifactReference(sessionID, sequence, artifact)
		if ref.ID != q.ID {
			out.Status = "changed"
			return out, nil
		}
		out.Items = []refbridge.Reference{ref}
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

	return out, nil
}

func (b *Bot) modelContextReferences(ctx context.Context, sessionID string) refbridge.Page {
	page, err := b.contextReferences(ctx, sessionID, refbridge.Query{Limit: refbridge.MaxPageRecords})
	if err != nil {
		return refbridge.Page{SessionID: sessionID, Unavailable: true}
	}
	return page
}
