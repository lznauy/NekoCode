package compact

import (
	"fmt"
	"strings"

	"nekocode/common"
	"nekocode/llm/types"
)

// NO_TOOLS_PREAMBLE prevents the summarizer model from making tool calls.
const NO_TOOLS_PREAMBLE = `CRITICAL: Respond with TEXT ONLY. Do NOT call any tools.
- Do NOT use Read, Bash, Grep, Glob, Edit, Write, or ANY other tool.
- Tool calls will be REJECTED and will waste your only turn — you will fail the task.
`

// FormatMessages formats a slice of messages into [role]: content lines,
// skipping empty/cleared entries and truncating long content.
func FormatMessages(msgs []types.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		content := strings.TrimSpace(m.Content)
		if content == "" || content == "." || content == ClearedMarker {
			continue
		}
		limit := 500
		if m.Role == "tool" {
			limit = 800
		}
		fmt.Fprintf(&b, "[%s]: %s\n", m.Role, common.TruncateByRune(content, limit))
	}
	return b.String()
}

// BuildPrompt assembles a structured summarization prompt from messages.
func BuildPrompt(msgs []types.Message, prevSummary string) string {
	conversation := FormatMessages(msgs)

	template := NO_TOOLS_PREAMBLE + `
You are a context summarization assistant for coding sessions.
Summarize only the conversation history provided below.
If a previous summary exists, update it incrementally — add new information and remove superseded items.
Do NOT mention that you are summarizing or compacting context.

CRITICAL Preservation Rules:
- Code snippets: preserve FULL code for any file that was modified or is under discussion.
- Error messages: copy VERBATIM — do NOT paraphrase. Exact error text enables accurate future diagnosis.
- File paths: always include the exact path with line numbers when available (e.g., "bot/agent/run.go:212").
- User directives and constraints: preserve all user-specified rules, preferences, and prohibitions.

Previous summary (if any):
` + prevSummary + `

Conversation to summarize:
` + conversation + `

Output your response in the following format:

<analysis>
Organize your thoughts here. Identify the key themes, decisions, and outcomes.
This section is a scratchpad and will be stripped — write freely.
</analysis>

<summary>
The compressed summary text that will replace the original messages in context.
Write concisely but include ALL code snippets and error messages verbatim.
</summary>

<key-facts>
- Fact 1: one-line established fact about the project or environment
- Fact 2: another fact
Only include facts that are confirmed true and likely relevant to future turns. Limit 5 facts.
</key-facts>`

	return template
}

// FormatCompactSummary extracts the <summary> block from LLM output.
func FormatCompactSummary(raw string) string {
	return extractXMLBlock(raw, "summary")
}

func extractXMLBlock(raw, tag string) string {
	openTag := "<" + tag + ">"
	closeTag := "</" + tag + ">"
	start := strings.Index(raw, openTag)
	if start < 0 {
		return ""
	}
	start += len(openTag)
	end := strings.Index(raw[start:], closeTag)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(raw[start : start+end])
}

