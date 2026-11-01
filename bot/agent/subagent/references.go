package subagent

import (
	"context"
	"fmt"
	"slices"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/refbridge"
	"nekocode/protocol"
)

// The task scope survives attempts, but cannot alias the parent or a sibling.
func referenceScope(cfg RunConfig) string {
	return cfg.SessionID + "/subagents/" + cfg.SubagentID
}

func referenceReader(cfg RunConfig, mgr *ctxmgr.Manager, store *core.OutputStore) refbridge.Reader {
	if cfg.SessionID == "" || cfg.SubagentID == "" || cfg.AttemptID == "" || store == nil || (!slices.Contains(cfg.Profile.Tools, "context_refs") && !slices.Contains(cfg.Profile.Tools, "evidence")) {
		return nil
	}
	return func(ctx context.Context, q refbridge.Query) (refbridge.Page, error) {
		tool := "context_refs"
		if q.Kind == refbridge.EvidenceKind {
			tool = "evidence"
		} else if q.Kind != "" {
			return refbridge.Page{}, fmt.Errorf("invalid reference kind")
		}
		if !slices.Contains(cfg.Profile.Tools, tool) {
			return refbridge.Page{}, fmt.Errorf("reference tool unavailable")
		}
		identity := protocol.ExecutionFromContext(ctx)
		if identity.TaskID != cfg.SubagentID || identity.AttemptID != cfg.AttemptID || core.OutputStoreFromContext(ctx) != store {
			return refbridge.Page{}, fmt.Errorf("reference actor unavailable")
		}
		base, tail, lazy := mgr.TranscriptTail()
		if !lazy {
			base = 0
		}
		return cfg.References.Read(ctx, referenceScope(cfg), q, base, tail)
	}
}
