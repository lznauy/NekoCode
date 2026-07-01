# Runtime Module Map

`runtime` owns the main agent loop and keeps the root package focused on
agent orchestration. Model calls and tool execution live in the two remaining
runner packages.

Start with `loop.go`, then follow one turn through `turn.go` into `model/` or
`toolrun/`.

## Root Package

| File | Responsibility |
| --- | --- |
| `agent.go` | Public `Agent` API, dependency wiring, lifecycle methods, hints, token/tool/governance accessors |
| `loop.go` | Top-level run loop, stop evaluation, final result selection, reusable `RunLoop` driver |
| `turn.go` | One agent turn: pre-turn hooks, interruption handling, model result routing, text completion, PostTurn hooks |
| `state.go` | Internal lifecycle, run, stream, token, and response-gate state |
| `host.go` | Adapter exposing `Agent` state to `model` and `toolrun` runners |

Root-package tests follow the implementation area they exercise:

| Test file | Coverage |
| --- | --- |
| `agent_hints_test.go` | Agent hint injection and transient context hints |
| `state_test.go` | Internal state helpers such as response-gate retry behavior |
| `tool_policy_test.go` | Runtime/toolrun integration for pre-tool edit/write policy |
| `turn_test.go` | Text-result handling inside one turn |

## Subpackages

| Directory | Responsibility |
| --- | --- |
| `model/` | LLM calls, retry wrapper, stream callbacks, response classification, PreModelRequest hooks, fallback synthesis |
| `toolrun/` | Tool-call filtering, execution, result recording, hook feedback, task sub-agent callback routing, sub-agent slots |

## Boundaries

- `runtime` may depend on `model` and `toolrun`.
- `model` and `toolrun` must not import `runtime`; they communicate through
  small host interfaces.
- Sub-agent code reuses `runtime.RunLoop` and `model.CallLLMWithRetry`, but
  keeps its own result formatting and safety classification.
