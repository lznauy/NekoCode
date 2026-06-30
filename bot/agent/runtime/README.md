# Runtime Module Map

`runtime` is the main agent loop. Start with `run_loop.go`, then follow the
branch into reasoning, tool execution, or final synthesis. Leaf concerns live in
small subpackages so the root package stays focused on orchestration.

## Root Package

| File | Responsibility |
| --- | --- |
| `agent.go` | Agent state, constructor, lifecycle control |
| `agent_api.go` | Public wiring methods for callbacks, hooks, tokens, tool state |
| `run_loop.go` | Top-level `Run` loop, stop evaluation, final result selection |
| `turn.go` | One turn: pre-turn hooks, interruption handling, tool/text branch |
| `reason.go` | Agent-side LLM call, streaming callbacks, context feedback |
| `tool_pipeline.go` | Tool execution pipeline and PostTool hook handoff |
| `tool_filter.go` | Tool quota checks and PreToolUse policy filtering |
| `tool_results.go` | Governance recording and context feedback for tool results |
| `tool_subagents.go` | Task/sub-agent callback routing |
| `postturn_hooks.go` | PostTurn hook evaluation and final-answer blocking |
| `hints.go` | Hint injection and steering drain |
| `final_synthesis.go` | Fallback synthesis when no final text exists |

## Subpackages

| Directory | Responsibility |
| --- | --- |
| `control/` | Governance retry gate for final-answer policy blocks |
| `messages/` | Runtime user-facing message and policy hint constants |
| `reasoning/` | Pure LLM response classification and garbled tool-call detection |
| `subagents/` | Sub-agent concurrency slots and color assignment |
| `toolflow/` | Pure tool-call/result ordering and callback conversion helpers |
| `toolpolicy/` | Pure tool target and edit-anchor policy helpers |
