package toolpolicy

import (
	"context"
	"fmt"
	"maps"

	"github.com/google/uuid"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/workspace"
	"nekocode/bot/policy"
	"nekocode/bot/policy/builtin"
)

type writeDraft struct {
	id, path, content string
}

// WriteDrafts belongs to a single actor's run. The serial governance pass
// prepares calls before concurrent execution, previews and permission checks.
type WriteDrafts struct {
	recent []writeDraft
}

func (d *WriteDrafts) Check(ctx context.Context, gov *policy.Policy, manager *workspace.Manager, call *core.ToolCallItem) Decision {
	if call.Name != "write" || call.EffectiveName != "" {
		return Check(ctx, gov, manager, *call)
	}
	path, _ := call.Args["path"].(string)
	rawRef, reuse := call.Args["reuse"]
	ref, _ := rawRef.(string)
	if reuse {
		if _, hasContent := call.Args["content"]; hasContent {
			return Decision{BlockReason: "Supply either content or reuse, not both."}
		}
		var found bool
		for _, draft := range d.recent {
			if draft.id == ref && draft.path == path {
				args := maps.Clone(call.Args)
				delete(args, "reuse")
				args["content"] = draft.content
				args["_existing_draft"] = true
				call.Args = args
				found = true
				break
			}
		}
		if !found {
			return Decision{BlockReason: "Unknown write draft for this run and path; supply full content."}
		}
	}
	decision := Check(ctx, gov, manager, *call)
	content, ok := call.Args["content"].(string)
	if decision.Terminated || decision.ReasonCode != builtin.ReadBeforeWrite || !ok || len(content) > 256<<10 {
		return decision
	}
	if !reuse {
		ref = uuid.NewString()
		if len(d.recent) == 4 {
			d.recent = d.recent[1:]
		}
		d.recent = append(d.recent, writeDraft{id: ref, path: path, content: content})
	}
	decision.BlockReason += fmt.Sprintf(" 草稿已保留。读取后若原提案仍适用，用相同 path 和 reuse=%q 重新调用 write；需调整则传新 content。", ref)
	return decision
}
