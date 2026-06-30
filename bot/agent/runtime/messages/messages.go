package messages

// -- fallback messages (shown to user when normal path fails) ---------------

const FallbackSynthesize = "Unable to produce a final summary — the model is currently unavailable. The task's tool operations may have completed, but the results could not be synthesized. Please try again or check the conversation log for details."

const FallbackNoAction = "Sorry, I couldn't determine what to do"

const MsgInterrupted = "Interrupted"

// -- policy hint messages (injected as hints to the LLM) -------------------

const PolicyBlockFinal = "final answer blocked by policy"
const PolicyBlockedDefault = "blocked by policy"

func PolicyRequireTool(tool, reason string) string {
	if tool != "" {
		return "必须先调用 " + tool + "：" + reason
	}
	return reason
}

func PolicyBlockedStop(stop string) string {
	return "blocked by stop policy: " + stop
}
