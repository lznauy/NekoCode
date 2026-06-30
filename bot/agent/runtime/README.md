# Runtime Module Map

`runtime` is the main agent loop. Start with `run_loop.go`, then follow the
delegation into the turn, model, or tool runners. Leaf concerns live in small
subpackages so the root package stays focused on orchestration.

## Root Package

| File | Responsibility |
| --- | --- |
| `agent.go` | Agent state, constructor, lifecycle control, runner wiring |
| `agent_deps.go` | Runtime dependency bundle for context, model, tools, governance |
| `api_callbacks.go` | Public callback and phase wiring methods |
| `api_governance.go` | Public governance and hook registry wiring methods |
| `api_tools.go` | Public tool executor wiring methods |
| `api_tokens.go` | Public token usage methods |
| `run_loop.go` | `loopRunner`: top-level run loop, stop evaluation, final result selection |
| `lifecycle_state.go` | Context cancellation, steering queue, finished flag, run duration |
| `turn.go` | `turnRunner`: pre-turn hooks, interruption handling, tool/text branch |
| `text_result.go` | Text response completion and recordability handling |
| `types.go` | Runtime-facing aliases for reasoning result/action types |
| `runner_host.go` | Adapter between `Agent` state and runner subpackages |
| `postturn_hooks.go` | `turnRunner`: PostTurn hook evaluation and final-answer blocking |
| `hints.go` | Hint injection and steering drain |
| `run_state.go` | Mutable per-run loop state |
| `stream_state.go` | Streaming callbacks and latest model reasoning state |
| `token_meter.go` | Token accounting and per-turn snapshots |

## Subpackages

| Directory | Responsibility |
| --- | --- |
| `control/` | Governance retry gate for final-answer policy blocks |
| `messages/` | Runtime user-facing message and policy hint constants |
| `modelrun/` | Model runner: LLM calls, stream callbacks, PreModelRequest hooks, fallback synthesis |
| `reasoning/` | Pure LLM response classification and garbled tool-call detection |
| `subagents/` | Sub-agent concurrency slots and color assignment |
| `toolflow/` | Pure tool-call/result ordering and callback conversion helpers |
| `toolpolicy/` | Pure tool target and edit-anchor policy helpers |
| `toolrun/` | Tool execution runner: filtering, execution, results, sub-agent callback routing |
