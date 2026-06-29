package runtime

import "fmt"

// -- guard messages (injected into context with Source: "system") -----------

const GuardReadOnlySpiral = "You've been reading without acting. Summarize your findings now — don't read any more files."

func ToolResultWarning(count int) string {
	return fmt.Sprintf("%d tool results accumulated. Check for unfinished sub-tasks — if any, continue with task. If all done, call task(verify) to validate, then report results.", count)
}

// -- fallback messages (shown to user when normal path fails) ---------------

const FallbackSynthesize = "Unable to produce a final summary — the model is currently unavailable. The task's tool operations may have completed, but the results could not be synthesized. Please try again or check the conversation log for details."

const FallbackNoAction = "Sorry, I couldn't determine what to do"

const MsgInterrupted = "Interrupted"

// -- policy hint messages (injected as hints to the LLM) -------------------

const PolicyBlockFinal     = "final answer blocked by policy"
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
