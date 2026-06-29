package app

import "nekocode/common"

func commandResult(pendingConfirm, sessionResumed bool) common.CmdResult {
	switch {
	case pendingConfirm:
		return common.CmdConfirming
	case sessionResumed:
		return common.CmdSessionResumed
	default:
		return common.CmdHandled
	}
}
