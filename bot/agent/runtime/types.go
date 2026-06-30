package runtime

import "nekocode/bot/agent/runtime/reasoning"

type ActionType = reasoning.ActionType
type ReasoningResult = reasoning.Result

const (
	ActionChat        = reasoning.ActionChat
	ActionExecuteTool = reasoning.ActionExecuteTool
)
