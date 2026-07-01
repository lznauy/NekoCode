package toolrun

import (
	"fmt"

	"nekocode/common/debug"
	"nekocode/bot/tools/core"
	"nekocode/bot/tools"

	"github.com/google/uuid"
)

type subSlotInfo struct {
	subID    string
	colorIdx int
}

func (r *Runner) prepareSubagentCallbacks(allowed []core.ToolCallItem, callback Callback) func() {
	var taskInfos []subSlotInfo
	for i, c := range allowed {
		if c.Name != "task" {
			continue
		}
		subType, _ := c.Args["type"].(string)
		if subType == "" {
			subType = "executor"
		}
		subID := uuid.New().String()
		colorIdx, ok := r.host.SubSlots().Acquire(subID, subType)
		if !ok {
			debug.Log("subSlotMgr: Acquire failed for %s (all slots full)", subType)
			continue
		}
		if callback != nil {
			callback("sub_agent_start", subType, subID, fmt.Sprint(colorIdx))
		}
		sid := subID
		cid := colorIdx
		taskInfos = append(taskInfos, subSlotInfo{sid, cid})
		allowed[i].Args["_sub_callback"] = tools.TaskCallbackFn(func(action, toolName, toolArgs, output string) {
			if callback == nil {
				return
			}
			sidTag := fmt.Sprintf("%s:%d", sid, cid)
			switch action {
			case "sub_tool_start":
				callback(action, toolName, toolArgs, sidTag)
			case "sub_execute_tool":
				callback(action, toolName, sidTag, output)
			default:
				callback(action, toolName, toolArgs, output)
			}
		})
	}

	return func() {
		for _, ti := range taskInfos {
			r.host.SubSlots().Release(ti.subID)
			if callback != nil {
				callback("sub_agent_end", "", ti.subID, "")
			}
		}
	}
}
