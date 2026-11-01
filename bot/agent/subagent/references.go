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
	if cfg.SessionID == "" || cfg.SubagentID == "" || cfg.AttemptID == "" || store == nil || !slices.Contains(cfg.Profile.Tools, "context_refs") {
		return nil
	}
	return func(ctx context.Context, q refbridge.Query) (refbridge.Page, error) {
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
