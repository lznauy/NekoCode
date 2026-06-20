package apistate

import "nekocode/common"

func CommandResult(pendingConfirm, sessionResumed bool) common.CmdResult {
	switch {
	case pendingConfirm:
		return common.CmdConfirming
	case sessionResumed:
		return common.CmdSessionResumed
	default:
		return common.CmdHandled
	}
}
