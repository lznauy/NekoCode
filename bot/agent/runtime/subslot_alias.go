package runtime

import subslotpkg "nekocode/bot/agent/subslot"

type SubSlotManager = subslotpkg.Manager

func NewSubSlotManager() *SubSlotManager {
	return subslotpkg.NewManager()
}
