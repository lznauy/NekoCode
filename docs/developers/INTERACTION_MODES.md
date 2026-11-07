# Interaction modes

Interaction modes describe how a task is handled. They are independent of permission modes (`manual`, `auto`, `full`), model selection and transcript scrolling.

## Current data flow

```text
TUI / GUI / HTTP / slash commands
                ↓
Bot session selection (interaction_mode)
                ↓
Mode registry → immutable run snapshot
                ↓
Direct strategy → agent → executor tool ceiling + existing authorization
```

`mode` owns immutable definitions: stable ID, version, role, execution strategy, tool policy and prompt. Built-in Standard and Plan both use the direct strategy. Unknown IDs and mismatched policy snapshots fail closed. Registry values and context snapshots are detached values.

The Bot owns selection and persists it in the session snapshot. Missing fields mean Standard. New sessions reset to Standard; forks retain the source selection. Empty conversations remain absent from session listings under the existing persistence rules. A selected mode remains in memory until a real conversation is saved. Unknown restored IDs are preserved so history remains viewable and `/mode standard` can recover explicitly.

Runtime resolves the effective snapshot after command dispatch, only when an input will reach the agent. Its mutation boundary blocks selection changes while running or waiting for human interaction. The snapshot travels in context, the accepted-input event and the recorded run view. Steering does not replace it. `/plan <task>` selects Plan for one run without changing the saved preference. Control commands do not become conversation messages.

The executor validates snapshots before installing their policy. Plan's read-only ceiling reuses registered `PlanAllowed` classifications and applies before normal approvals. Full access cannot bypass it. The legacy `SetPlanMode` API derives the same policy rather than maintaining another flag. Run cleanup clears the active policy and runtime prompt, including cancellation/error paths, while retaining the session preference.

## UI and transport contracts

- TUI: the selector precedes Follow, opens with mouse or Ctrl+O, reuses Suggestions and never replaces the draft. Draft edits return keyboard focus from the mode picker.
- GUI: selector options come from the runtime, not a frontend enum; changes are confirmed by the backend and controls are locked during a pending mutation or run.
- HTTP: `GET /mode` returns `{selected, options}`; `POST /mode` accepts `{mode: "plan"}`. The existing auth middleware covers both. Busy changes return conflict; unsupported modes are rejected.
- `interaction_mode_changed` is a global event; commands and direct mutations both publish it. Clients without the optional mode capability continue using the unchanged run interface and slash commands.
- `AccessObservation.Mode` keeps its existing permission meaning; `PlanMode` remains a derived compatibility observation.

## Adding a mode

1. Add its definition to the registry, using a new stable ID and a version for its complete policy.
2. Bind its execution strategy in the Bot dispatch boundary before exposing it. Standard/Plan only bind `direct` today; unsupported strategies cannot fall through to direct execution.
3. Implement and validate the role/tool ceiling in the executor or the corresponding execution host. Prompts never grant capabilities.
4. Test selection persistence, cancellation, unknown versions, UI state, and interaction with existing permission modes. Generic selectors need no new mode-specific branches.

A future super-agent strategy can attach to the existing task/subagent runtime. Its coordinator and workers require different roles: workers inherit task authorization and permission ceilings, not a coordinator-only delegation restriction. Persisted tasks, attempts, event subscriptions and recovery belong in a task orchestration layer outside the mode registry. Changing the selected mode will affect new tasks, while existing task snapshots keep their policy; stopping them requires a separate operation. Proactive admission settings are independent of work modes and Plan.
