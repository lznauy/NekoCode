package runtime

import gatepkg "nekocode/bot/agent/gate"

type ResponseGate = gatepkg.ResponseGate

func NewResponseGate() *ResponseGate {
	return gatepkg.NewResponseGate()
}
