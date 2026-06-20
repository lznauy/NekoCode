# Bot Layer Refactor Plan

## Goal

Restructure `bot` so each directory maps to a clear business module, files have single, discoverable responsibilities, and package dependencies flow in one direction. This refactor intentionally does not preserve old internal import paths.

## Refactor Acceptance Rules

Every refactor batch must satisfy these rules before it is marked complete:

- Directory names describe a single business module, not an implementation bucket.
- Each production file has one primary responsibility that can be inferred from its directory and filename.
- Files that mix orchestration, parsing, persistence, formatting, policy, and side effects must be split.
- Each meaningful responsibility has direct test coverage in the same package or an adjacent black-box test.
- Cross-module calls flow through app composition, interfaces, or DTOs; lower-level modules must not import higher-level modules.
- A batch is not accepted if it only renames directories without reducing responsibility mixing or documenting why a file is already cohesive.

## Target Architecture

```text
bot/
  app/             # Bot assembly, lifecycle, command wiring, extension wiring
    apistate/      # public API result/status formatting helpers
    contextguard/  # tool-result guardrail injection
    contextinit/   # project context and index injection
    pluginops/     # plugin command parsing and response helpers
    pluginruntime/ # plugin agent/hook/MCP resource wiring
    sessioncmd/    # session command formatting/export helpers
    sessionstate/  # session/context snapshot mapping
    taskwire/      # delegated task run config and result mapping
  agent/           # Agent runtime
    runtime/       # run loop, turns, reasoning, tool execution, final handling
    governance/    # budget, ledger, gate, tool semantics, final checks
    subagent/      # sub-agent registry, engine, result handling
  contextmgr/      # context building, compaction, memory, token tracking, reports
  tools/           # tool protocol, registry, executor, execution state
    core/          # tool protocol DTOs, tool interface, descriptor formatting
    execution/     # per-run execution state, snapshot store wiring, file read cache
    llmstream/     # streamed LLM consumption and tool-call delta aggregation
    runner/        # tool execution batching, preview, confirmation, output shaping
    pathutil/      # path resolution and safe/normalized file reads
    textutil/      # ANSI stripping and text normalization
    netclient/     # shared HTTP client construction
    semantics/     # tool-call semantic classification
    snapshots/     # snapshot recording helpers
    filesystem/    # filesystem tool family
      read/         # text/media reads, read output formatting, snapshot recording
      write/        # file write/overwrite
      list/         # flat directory listing
      tree/         # directory tree rendering
      search/       # glob and content search
      edit/         # edit tool, block resolver, edit preview formatting
    shell/         # bash
    web/           # web_fetch/web_search/html2md
    media/         # image_gen
    task/          # task tool
    editdsl/       # hashline edit DSL
  hooks/           # hook registry, builtin hooks, plugin hooks
  index/           # project code index facade
    db/            # index persistence and FTS
    graph/         # code graph model and queries
    indexer/       # indexing orchestration
    parser/        # tree-sitter parsing
    projectctx/    # NEKOCODE/project context discovery
    projecttool/   # project_info tool adapter
    service/       # project index lifecycle manager
    syncer/        # filesystem watcher and incremental updates
  extension/       # extension systems
    plugin/
    mcp/
    skill/
  config/
  prompt/
    system/
    planmode/
  session/
  debug/
  sdk/
    volcengine/
  treesitter/
    languages/
```

## Dependency Rules

```text
app -> agent/runtime, contextmgr, tools, hooks, index, extension/*
agent/runtime -> agent/governance, tools, hooks, contextmgr
tools/* -> tools/editdsl
hooks -> governance semantics only when necessary
```

Lower-level modules must not import upper-level modules. `app` is the composition root and may depend on all modules. Other modules should communicate through interfaces or DTOs.

## Current File Responsibility Audit

This audit is the working checklist for the bot-layer refactor. It focuses on source files whose size, dependencies, or mixed behavior make them likely maintenance risks.

### Root / App Layer

| File | Current issue | Target split | Required tests |
| --- | --- | --- | --- |
| `bot/bot.go` | Composition root, session setup, command wiring, index/tools/hooks/plugin/MCP/skill wiring, and run helpers previously lived in root. | Completed move to `bot/app`; root `bot` now only aliases `app.Bot` and forwards `New`. | Full `./bot/...` tests cover the facade and app wiring compile path. |
| `bot/api.go`, `bot/api_*.go` | Public API methods previously lived in root with display conversion helpers mixed in. | Completed move of Bot API methods to `bot/app`; display conversion lives in `bot/sessionview`; root `bot` remains facade only. | `bot/sessionview/messages_test.go` covers session display filtering and persistent tool blocks. |
| `bot/plugin_commands.go` | Plugin command routing and Bot-side registry mutation previously lived in root. | Completed move of Bot command coordination to `bot/app`; Bot-free helpers live in `bot/plugincli`. | `bot/plugincli` tests cover source detection, install formatting, env expansion, and command output formatting. |
| `bot/app/session_persistence.go`, `bot/app/init_commands.go` | Session snapshot mapping, loaded-skill serialization, `/sessions` formatting, resume/export response text, and `/export` file writing were embedded in app methods. | Completed extraction of pure session state mapping to `bot/app/sessionstate` and session command helpers to `bot/app/sessioncmd`; app methods now coordinate Bot state only. | `sessionstate` tests cover context/session snapshot conversion and deterministic loaded-skill names; `sessioncmd` tests cover session list formatting, resume/export response text, and export file writing. |
| `bot/app/plugin_install.go`, `bot/app/plugin_confirm.go` | Plugin install argument parsing, remote manifest preview fetching/parsing, and confirmation summary formatting were mixed into Bot methods alongside goroutine/registry coordination. | Completed extraction of pure plugin operation helpers to `bot/app/pluginops`; app methods now coordinate registry, confirmation state, async install, and notifications. | `pluginops` tests cover install arg parsing, remote preview success/errors, unsupported preview URLs, and confirmation summary notes. |
| `bot/app/init_extensions.go` | Skill reload, plugin registry initialization, plugin agent registration, hook loading, MCP startup/tool registration, and plugin extension unloading were coupled. | Completed extraction of plugin resource load/unload behavior to `bot/app/pluginruntime`; `init_extensions.go` now coordinates app state and skill refresh only. | `pluginruntime` tests cover MCP tool/client-name matching; full app tests cover compile path for plugin runtime wiring. |
| `bot/app/init_agent.go` | LLM client setup, agent callback wiring, tool-result guardrail injection, task tool wiring, sub-agent run config construction, callback adaptation, token accounting, and result conversion were coupled. | Completed extraction of tool-result guardrail logic to `bot/app/contextguard` and delegated task wiring helpers to `bot/app/taskwire`; `init_agent.go` now focuses on agent construction and dependency wiring. | `contextguard` tests cover warning injection thresholds and intervals; `taskwire` tests cover run-config callback adaptation, unknown agent handling, and sub-agent result mapping. |
| `bot/app/plugin_manage.go` | Plugin enable/disable/info argument validation, not-found/already-enabled/already-disabled state messages, and failure/result formatting were embedded in Bot methods. | Completed extraction of plugin management validation and response formatting to `bot/app/pluginops`; app methods now perform registry mutations and extension load/unload only. | `pluginops/manage_test.go` covers plugin lookup outcomes and management response messages. |
| `bot/app/init_context.go` | Config/prompt setup, context manager creation, project context loading, index manager initialization, skeleton injection, and context-window finalization were coupled. | Completed extraction of project context and index injection flow to `bot/app/contextinit`; `init_context.go` now creates base context state and stores the initialization result. | `contextinit` tests cover empty cwd, project text injection, context-window finalization, and skeleton injection. |
| `bot/app/api.go`, `bot/app/api_stats.go` | Command result classification and duration display formatting were embedded in public API methods. | Completed extraction of pure API state helpers to `bot/app/apistate`; API methods now coordinate Bot state and delegate classification/formatting. | `apistate` tests cover command result priority and duration formatting. |

### Agent Runtime

| File | Current issue | Target split | Required tests |
| --- | --- | --- | --- |
| `bot/agent/run.go` | Turn loop, context updates, hook invocation, tool dispatch, final handling, and streaming coordination previously lived in root `agent`. | Completed move into `bot/agent/runtime`; root `agent` is now a compatibility facade. | Runtime tests cover turn lifecycle, text handling, and final-state behavior. |
| `bot/agent/run_exec.go` | Tool execution, permissions, context patching, hook state, and result shaping previously lived in root `agent`. | Completed move into `bot/agent/runtime` with focused execution files and tests. | Runtime tests cover filtering, result merge/order, post-tool hooks, and pre-edit policy. |
| `bot/agent/agent.go` | Agent construction and runtime state ownership previously lived in root `agent`. | Completed move into `bot/agent/runtime`; root aliases `runtime.Agent` and forwards `New`. | Runtime tests and full app compile path cover constructor behavior. |
| `bot/agent/reasoner.go` | Reasoning orchestration and synthesis were root `agent` runtime behavior. | Completed move into `bot/agent/runtime`; pure format policy remains in `bot/agent/reasoning`. | Runtime and reasoning tests cover LLM response classification and garbled tool-call detection. |
| `bot/agent/gate.go`, `bot/agent/gov.go`, `bot/agent/subslot.go` | Governance, response gating, and sub-agent slot management previously lived in root `agent`. | Completed move to `bot/agent/governance`, `bot/agent/gate`, and `bot/agent/subslot`; root `agent` keeps compatibility aliases only. | Dedicated tests now live with governance, gate, and subslot packages. |
| `bot/agent/subagent/engine.go` | Subagent scheduling, execution, result handling, and context preparation are in one file. | `subagent/scheduler.go`, `executor.go`, `context.go`, `result_handler.go`. | Scheduler, executor error, and result handling tests. |

### Hooks

| File | Current issue | Target split | Required tests |
| --- | --- | --- | --- |
| `bot/hooks/hooks.go` | Event types, registry state, evaluation pipeline, formatting, state patching, and match helpers were coupled. | Completed split into root `events.go`, `registry.go`, `state.go`, and `format.go`; root now owns registry state and adapters only. | Registry counts/reset, state patch, and hint formatting tests. |
| `bot/hooks/builtin.go` | Builtin hook policies were grouped by implementation convenience, not policy domain. | Completed move of policy implementation to `bot/hooks/builtin` with quota, verification, exploration, progress, and quality files; root keeps adapter/register wrappers. | `hooks/builtin/policy_test.go` covers policy behavior; root tests cover registration and adapter integration. |
| `bot/hooks/plugin.go` | Plugin hook schema, matching, runner invocation, conversion, and diagnostics were coupled in root hooks. | Completed move to `bot/hooks/plugin` with config, hook action execution, runner, matcher, schema, format, and local DTO files; root keeps `LoadPluginHooks` adapter. | Plugin package tests cover schema, matcher, output formatting, command runner, config loading, and action validation. |

### Tools

| File | Current issue | Target split | Required tests |
| --- | --- | --- | --- |
| `bot/tools/filesystem/edit/diff.go` | Diff preview orchestration used to mix hunk sorting, changed-line mapping, and full-file view rendering. | Completed split inside the `filesystem/edit` module into `diff.go`, `diff_hunks.go`, and `diff_lines.go`. | Covered through edit tool tests; add direct diff-format tests if output changes. |
| `bot/tools/filesystem/edit/tool_edit.go` | Tool schema, validation, hashline execution, diagnostics, and file cache interaction were coupled. | Completed split inside the `filesystem/edit` module into `tool_edit.go`, `edit_preflight.go`, `edit_commit.go`, `edit_lint.go`, and diff files. | Existing edit tool tests cover preview, execution, recovery, and cache behavior. |
| `bot/tools/shell/tool_bash.go` | Command validation, process execution, redirection parsing, and output formatting were coupled. | Completed split into `tool_bash.go`, `danger.go`, `redirection.go`, and `runner.go`. | Shell tests cover command execution and rejection behavior. |
| `bot/tools/filesystem/read/tool_read.go` | Path validation, file reading, pagination, image handling, and cache state lived together. | Completed module move and split into `tool_read.go`, `text.go`, `media.go`, and `suggest.go`. | Read tests cover text output and snapshot recording. |
| `bot/tools/media/tool_image_gen.go` | Tool schema, request construction, transport, and artifact handling were coupled. | Completed split into `tool_image_gen.go`, `image_model.go`, `jimeng.go`, and `image_artifacts.go`. | Media tests cover request defaults and artifact handling. |
| `bot/tools/types.go` | Tool protocol DTOs, interface definitions, descriptor conversion, and display arg formatting were mixed in root tools. | Completed move of protocol definitions and formatting to `bot/tools/core`; root `tools` keeps aliases/wrappers only. | `tools/core/format_test.go` covers descriptor conversion, arg formatting, and result output selection; root facade tests cover aliases. |
| `bot/tools/file_cache.go` | Per-run execution state, global snapshot/cache fallback, file cache entries, LRU eviction, cache transfer, and range merging were in one root file. | Completed move to `bot/tools/execution` with `state.go`, `cache.go`, `cache_transfer.go`, and `ranges.go`; root `tools/file_cache.go` is now a facade. | `tools/execution/cache_test.go` covers cache hits, invalidation, merge, eviction, and range merging. |
| `bot/tools/streaming.go` | Stream callbacks, token consumption, tool-call delta aggregation, LLM call wrapping, and ANSI cleanup were coupled in root tools. | Completed move to `bot/tools/llmstream` with type definitions, stream consumption, tool-call conversion, and LLM call wrapper files; root `tools/streaming.go` is now a facade. | `tools/llmstream/tool_calls_test.go` covers deterministic tool-call ordering and invalid-argument skipping. |
| `bot/tools/executor.go` | Executor state, callback setters, preview preparation/emission, parallel/sequential scheduling, single-call execution, confirmation policy, panic recovery, output truncation, and file-cache invalidation were coupled. | Completed move to `bot/tools/runner` with executor state, preview, batch scheduling, single-call execution, output, and path/confirmation helper files; root `tools/executor.go` is now a facade. | `tools/runner/executor_test.go` covers order preservation, forbidden/plan-mode blocking, confirmation denial, and truncation. |
| `bot/tools/util.go` | ANSI stripping, path resolution, file reads, edit DSL path extraction, HTTP client creation, exploratory semantics, and snapshot recording were mixed as a generic utility bucket. | Completed split into `textutil`, `pathutil`, `netclient`, `semantics`, and `snapshots`; root `tools/util.go` keeps compatibility wrappers plus edit DSL path wrappers. | Direct helper tests cover text normalization, path resolution/read normalization, HTTP client construction, and exploratory classification; root util tests cover compatibility wrappers. |

### Edit DSL

| File | Current issue | Target split | Required tests |
| --- | --- | --- | --- |
| `bot/tools/editdsl/apply.go` | Patch application used to bundle landing repair, boundary repair, delimiter repair, block resolution, and blank-line cleanup. | Completed split into `apply.go`, `apply_landing.go`, `apply_boundary.go`, `apply_delimiters.go`, `apply_blocks.go`, `apply_blanks.go`, and `apply_types.go`. | Split tests now cover apply, block, recovery, mapping, and integration behavior. |
| `bot/tools/editdsl/patch.go` | Patch parsing used to bundle type definitions, payload modeling, contamination diagnostics, and range parsing. | Completed split into `patch.go`, `types.go`, `parse_payload.go`, `parse_errors.go`, and `parse_range.go`. | Parser/payload tests are isolated in `parse_test.go`. |
| `bot/tools/editdsl/hashline_test.go` | Giant test file covered many responsibilities indirectly. | Completed split into responsibility-specific tests: `hash_test.go`, `parse_test.go`, `apply_test.go`, `snapshot_test.go`, `recovery_test.go`, `block_test.go`, `integration_test.go`, `mapping_test.go`. | Direct tests now align with edit DSL components. |

### Index

| File | Current issue | Target split | Required tests |
| --- | --- | --- | --- |
| `bot/index/parser.go` | Language config, tree-sitter parsing, symbol extraction, and parse helpers were mixed in the root index package. | Completed move to `bot/index/parser`; root `index` keeps facade parser aliases/wrappers only. | `bot/index/parser/parser_test.go` covers language parsing, docs, signatures, package path extraction, and parser setup. |
| `bot/index/graph.go` | Graph mutation, traversal, query, ranking, and formatting were in one file. | Completed move to `bot/index/graph` with `graph.go`, `mutate.go`, `lookup.go`, `format.go`, `language.go`, and `types.go`; root `index` keeps facade type aliases only. | `bot/index/graph/graph_test.go` covers mutation, lookup, dependency, file, and skeleton behavior. |
| `bot/index/db.go` | Schema, migrations, CRUD, transactions, graph loading, and search persistence were coupled. | Completed move to `bot/index/db` with connection lifecycle, schema, node/edge repository, file repository, graph loading, and FTS search files; root `index` keeps facade DB aliases/wrappers only. | `bot/index/db/db_test.go` covers persistence, graph loading, counts, deletion, clear, and FTS triggers. |
| `bot/index/index.go` | Index walk, stale detection, parsing orchestration, graph/db writes, reference resolution, file update, and search access were coupled. | Completed move to `bot/index/indexer` with `indexer.go`, `policy.go`, `walk.go`, `stale.go`, `file_update.go`, `references.go`, and `search.go`; root `index` keeps facade indexer aliases/wrappers only. | `bot/index/indexer/indexer_test.go` covers full indexing, stale rebuilds, reference resolution, ignored/generated files, and query behavior. |
| `bot/index/project.go` | Project-context discovery was mixed into the code-index package despite serving prompt context loading rather than graph indexing. | Completed move to `bot/index/projectctx`. | `projectctx/project_test.go` covers discovery, includes, and empty context behavior. |
| `bot/index/manager.go` | Project-root discovery, index lifecycle, graph ownership, syncer startup, query locking, and rebuild/close lifecycle were coupled in the root index package. | Completed move to `bot/index/service`; root `index.Manager` is now a facade alias. App initialization imports `index/service` directly. | Covered by project tool and full index tests; service compile path is covered by app initialization tests. |
| `bot/index/sync.go` | File watcher setup, directory filtering, debounce batching, event handling, DB updates, and graph locking were mixed into root index. | Completed move to `bot/index/syncer`; it now depends only on `indexer` and `graph`. Root `index.Syncer` is now a facade alias. | `bot/index/syncer/syncer_test.go` covers supported file create/remove watcher events and unsupported-file filtering. |
| `bot/index/tool.go` | Tool adapter schema, manager access, query parsing, graph query formatting, FTS lookup, and path display shortening were coupled in root index. | Completed move to `bot/index/projecttool`; app tool registration imports `index/projecttool` directly. Root `index.ProjectInfoTool` is now a facade alias. | `bot/index/projecttool/tool_test.go` covers skeleton, symbol, file, deps, search, invalid query, nil graph, path shortening, and tool metadata. |

### Extension Systems

| File | Current issue | Target split | Required tests |
| --- | --- | --- | --- |
| `bot/plugin/registry.go` | Discovery, manifest loading, install state, enable/disable, install/uninstall, and resource auto-discovery were coupled in one root package file. | Completed move to `bot/extension/plugin`; split into `manifest.go`, `model.go`, `discovery.go`, `state.go`, `install.go`, `exec.go`, and `registry.go`. Root `bot/plugin` is now a facade. | Existing plugin tests moved with the implementation and cover manifest parsing, registry state, install preview, copy/exec helpers, resource discovery, and enable/disable lifecycle. |
| `bot/mcp/client.go` | Process management, JSON-RPC protocol, tool discovery, and request execution were coupled. | Completed move to `bot/extension/mcp`; split into `types.go`, `process.go`, `protocol.go`, `tools.go`, and `tool.go`. Root `bot/mcp` is now a facade. | MCP fake-server and tool adapter tests moved with the implementation and cover process lifecycle, RPC handshake, tool listing/call behavior, danger parsing, and adapter parameters. |
| `bot/skill/loader.go`, `bot/skill/skill.go`, `bot/skill/tool_skill.go` | Skill model/registry, directory discovery, file loading, frontmatter parsing, context formatting, and tool adapter behavior were coupled in the root skill package. | Completed move to `bot/extension/skill`; split into `model.go`, `registry.go`, `discovery.go`, `load.go`, `parse.go`, `format.go`, and `tool_skill.go`, with bundled skills under `extension/skill/bundled`. Root `bot/skill` and `bot/skill/bundled` are now facades. | Skill tests moved with implementation and cover discovery/load, parse errors, registry behavior, context formatting, and skill tool execution. |

### Context Manager

| File | Current issue | Target split | Required tests |
| --- | --- | --- | --- |
| `bot/contextmgr/manager.go` | Manager state, build flow, compaction trigger, storage access, statistics, and snapshots were mixed. | Completed split into `manager.go`, `settings.go`, `build.go`, `storage.go`, `history.go`, `compaction.go`, `stats.go`, `token_usage.go`, `snapshot.go`, and `report.go`. | Existing context manager tests cover storage, build filtering, reports, snapshots, context-window settings, and auto-compaction entry points. |
| `bot/contextmgr/memory/memory.go` | Memory persistence, parsing, formatting, append, merge policy, and field mapping were coupled. | Completed split inside `bot/contextmgr/memory` into model, load, save, build, append, merge, parse, and field mapping files. | `memory_test.go` covers load/save, build, append, merge, default path, and complex parsing behavior. |

### Support Modules

| File | Current issue | Target split | Required tests |
| --- | --- | --- | --- |
| `bot/debug/log.go` | Global file setup, log rotation, caller formatting, normal debug logging, and sub-agent logging were coupled and untested. | Completed split into `logger.go`, `file.go`, and `format.go`. | `debug/logger_test.go` covers timestamped log output and rotation behavior with temp files. |
| `bot/prompt/builder.go` | Embedded system prompt, environment block construction, OS release parsing, analysis rules, and plan-mode prompt template were mixed. | Completed split into `prompt/system` for system prompt/env/analysis rules and `prompt/planmode` for plan-mode template; root `prompt` is a facade. | `prompt/system` tests cover deterministic env prompt construction and OS release parsing; `prompt/planmode` tests cover task injection and write-blocking rules. |
| `bot/sdk/volcengine_signer.go` | Volcengine signer mixed public API, canonical request construction, signing-key derivation, hashing/HMAC helpers, and wall-clock time. | Completed move to `bot/sdk/volcengine` with `signer.go`, `canonical.go`, and `crypto.go`; root `sdk` is a facade. | `sdk/volcengine/signer_test.go` covers deterministic signature metadata, payload hash, credential scope, signed headers, and canonical query escaping. |
| `bot/treesitter/langs.go` | Shared language map and parser factory were untested in the root tree-sitter package. | Completed move to `bot/treesitter/languages`; root `treesitter` is a facade. | `treesitter/languages/languages_test.go` covers supported extensions and parser creation. |

### Remaining Guardrails

The main high-risk modules now have direct tests. These remaining guardrails keep the architecture from drifting back into mixed responsibilities:

- `bot/app/*` composition-root wiring files are intentionally thin and covered by full `./bot/...` compile/test paths; pure app helpers now live in tested subpackages such as `app/sessionstate` and `app/sessioncmd`.
- Root facade packages (`bot`, `bot/agent`, `bot/index`, `bot/plugin`, `bot/mcp`, `bot/skill`, `bot/tools`) should stay thin and should not accumulate new behavior.
- Event-driven modules such as `bot/index/syncer` should keep direct behavior tests whenever debounce, filtering, or mutation policy changes.

## Task Log

### Batch 1: Context Manager Rename

Status: completed

Scope:
- Move `bot/ctxmgr` to `bot/contextmgr`.
- Update all imports from `nekocode/bot/ctxmgr...` to `nekocode/bot/contextmgr...`.
- Keep behavior unchanged.
- Run focused and full bot tests.

Completed changes:
- Moved the directory to `bot/contextmgr`.
- Renamed the root package to `contextmgr`.
- Updated internal imports to `nekocode/bot/contextmgr...`.
- Updated stale documentation references outside this refactor log.

Completion criteria:
- No `bot/ctxmgr` directory remains.
- No imports reference `nekocode/bot/ctxmgr`.
- Focused context manager tests pass.

Verification:
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/contextmgr/... ./bot/agent ./bot/command ./bot`

### Batch 2: Code Index Rename

Status: completed

Scope:
- Move `bot/cindex` to `bot/index`.
- Update imports and package names.
- Keep tool behavior unchanged.

Completed changes:
- Moved the directory to `bot/index`.
- Renamed package declarations to `index`.
- Updated imports and default index database path from `.nekocode/cindex.db` to `.nekocode/index.db`.

Completion criteria:
- No production imports reference `nekocode/bot/cindex`.
- Focused index tests pass.
- This batch is recorded as a naming foundation only; responsibility splits are tracked separately in the index audit above.

Verification:
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/index ./bot`

### Batch 3: Root App Decomposition

Status: completed

Scope:
- Split `bot/bot.go`, `bot/api.go`, and `bot/plugin_commands.go` by responsibility.
- Create `bot/app` as the composition root.
- Add direct tests for app construction, extension wiring, public API defaults, and plugin command parsing/formatting.

Completed changes:
- Reduced `bot/bot.go` to the `Bot` state type and `New` construction sequence.
- Split root initialization into `init_context.go`, `init_tools.go`, `init_extensions.go`, `init_agent.go`, `init_commands.go`, and `session_persistence.go`.
- Split public API behavior into facade, run/callback, stats, model switching, and session-message conversion files.
- Split plugin command behavior into command routing, install flow, confirmation/formatting, and plugin management files.
- Moved the `Bot` type, root initialization files, public API methods, plugin command coordination, and session persistence into `bot/app`.
- Reduced root `bot/bot.go` to a compatibility facade: `type Bot = app.Bot` and `New()`.
- Added `bot/plugincli` and moved plugin command usage/source classification, install preview/result formatting, plugin list/info formatting, and remote manifest fetching out of root `bot`.
- Added `bot/sessionview` and moved session display-message filtering, assistant tool-block grouping, and internal-message suppression out of root `bot`.
- Moved plugin MCP environment expansion into `bot/plugincli` so `init_extensions.go` only coordinates extension wiring.
- Moved root plugin command helper tests into `bot/plugincli`.
- Added direct tests for plugin install formatting and session display conversion helpers.
- Added `bot/app/sessionstate` for context/session snapshot mapping and loaded-skill serialization.
- Added `bot/app/sessioncmd` for `/sessions` list formatting and `/export` file writing helpers.
- Reduced `session_persistence.go` and session command registration to Bot-state coordination over tested helper modules; resume/export response formatting also lives in `sessioncmd`.
- Added `bot/app/pluginops` for plugin install arg parsing, remote manifest preview parsing, and install confirmation summary formatting.
- Reduced plugin install/confirmation methods to Bot-state coordination over tested helper modules.
- Added `bot/app/pluginruntime` for plugin agent/hook/MCP resource loading and unloading.
- Reduced `init_extensions.go` plugin extension methods to runtime delegation.
- Added `bot/app/contextguard` for tool-result guardrail counting and warning injection.
- Added `bot/app/taskwire` for sub-agent run config construction, callback adaptation, and result conversion.
- Reduced `init_agent.go` to LLM/agent construction and dependency wiring.
- Expanded `bot/app/pluginops` to cover plugin enable/disable/info lookup and management response formatting.
- Removed app-local plugin argument lookup helper after moving validation into `pluginops`.
- Added `bot/app/contextinit` for project-context loading, index manager initialization, skeleton injection, and context-window finalization.
- Reduced `init_context.go` to base config/prompt/context construction and result assignment.
- Added `bot/app/apistate` for command result classification and stats duration formatting.
- Reduced public API methods to Bot state coordination plus helper delegation.

Verification so far:
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot`
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot ./bot/plugincli`
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot ./bot/sessionview ./bot/plugincli`
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot ./bot/app ./bot/plugincli`
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/app/... ./bot/app ./bot`
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/app/... ./bot/app ./bot/plugincli`
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/app/... ./bot/app`
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/app/contextguard ./bot/app/taskwire ./bot/app`
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/app/... ./bot/app`
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/app/... ./bot/app`
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/app/... ./bot/app`

### Batch 4: Agent Runtime And Governance Decomposition

Status: completed

Scope:
- Split runtime orchestration from policy decisions.
- Move gate/governance/reasoning/subslot policy into explicit governance files or package.
- Split subagent engine into scheduling, execution, context, and result responsibilities.
- Add direct tests for each policy and runtime boundary.

Completed changes:
- Reduced `agent/run.go` to the high-level run loop and turn orchestration.
- Moved text-response handling to `agent/run_text.go`.
- Moved PostTurn hook handling to `agent/run_postturn.go`.
- Moved final-answer governance checks to `agent/run_final.go`.
- Moved hint injection, forced synthesis, and steering drain helpers to `agent/run_context.go`.
- Reduced `agent/run_exec.go` to tool execution orchestration.
- Split tool execution filtering, subagent callback wiring, result feedback, post-tool hooks, and pre-edit guard into focused files.
- Split `agent/gov.go` into governance type definitions, lifecycle/ledger sync, tool recording, final check, and observability files.
- Added direct tests for tool filtering, result ordering, post-tool stop handling, governance lifecycle state, and tool recording state.
- Split `agent/subagent/engine.go` into run orchestration, state/meta, context/prompt setup, executor/tool feedback, LLM reasoning, result builders, and safety classification files.
- Split synthesis, garbled-output detection, and retry helpers out of `agent/reasoner.go`.
- Moved response retry gate implementation into `bot/agent/gate` with root compatibility alias.
- Moved sub-agent slot manager implementation into `bot/agent/subslot` with root compatibility alias.
- Moved governance manager implementation into `bot/agent/governance`, including lifecycle hook sync, ledger recording, final-answer checks, and observability.
- Moved garbled tool-call format detection into `bot/agent/reasoning` with root compatibility wrapper.
- Moved the `Agent` implementation, run loop, tool execution flow, reasoning, synthesis, retry helper, and runtime tests into `bot/agent/runtime`.
- Reduced root `bot/agent` to a compatibility facade that aliases runtime types and forwards constructors.
- Added direct tests for subagent prompt construction, read-only spiral guard, sensitive call detection, response gate, sub-slot manager, and garbled tool-call detection.

Verification:
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/agent`
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/agent ./bot/agent/subagent`
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/agent/...`

### Batch 5: Hook System Responsibility Split

Status: completed

Scope:
- Split `hooks.go` into event definitions, registry, evaluation, state patching, formatting, and match helpers.
- Split builtin hook policies by event/policy domain.
- Split plugin hook schema, matching, runner, conversion, and diagnostics.
- Audit all hook injection conditions after the split with direct tests for event order, state patch behavior, and plugin matching.

Completed changes:
- Replaced the mixed `hooks.go` file with `events.go`, `state.go`, `registry.go`, and `format.go`.
- Split builtin hooks into registration, quota, verification/circuit-breaker, exploration, progress-stall, and completion-quality files.
- Split plugin hooks into config loading, hook action execution, output formatting, schema validation, command runner, and matcher files.
- Moved builtin hook policy implementation into `bot/hooks/builtin`; root hooks now adapts `Snapshot` to the builtin `State` interface and preserves the existing `RegisterBuiltin` entry point.
- Moved builtin policy behavior tests into `bot/hooks/builtin/policy_test.go`; root hooks now keeps only registration/adapter tests for builtins.
- Moved plugin hook implementation into `bot/hooks/plugin`; root hooks now adapts plugin-local DTOs back to root `Hook`/`Result` types via `LoadPluginHooks`.
- Added responsibility-focused tests for registry counts/reset, snapshot state patches, hint formatting, builtin registration, plugin matching, plugin action validation, and plugin command execution.
- Removed the broad legacy `hooks_test.go` and `plugin_test.go` files.
- Split hook tests into policy and infrastructure files matching the production responsibilities.

Verification:
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/hooks`
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/hooks ./bot/hooks/builtin ./bot/hooks/plugin`
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/...`
- `env GOCACHE=/tmp/nekocode-go-build go vet ./bot/...`
- `git diff --check -- bot/hooks docs/BOT_REFACTOR.md`

### Batch 6: Tools Module Split

Status: completed

Scope:
- Keep `bot/tools` as protocol/registry/executor only.
- Move builtin tools into filesystem/shell/web/media/task packages.
- Rename `hashline` to `editdsl`.
- Split large tool files by validation, execution, result formatting, and shared state.
- Add tests matching each responsibility.

Completed changes:
- Removed the old `bot/tools/builtin` bucket directory.
- Added module directories:
  - `bot/tools/catalog` for built-in tool registration.
  - `bot/tools/filesystem/read` for text/media reads, read output formatting, and snapshot recording.
  - `bot/tools/filesystem/write` for file write/overwrite.
  - `bot/tools/filesystem/list` for flat directory listing.
  - `bot/tools/filesystem/tree` for directory tree rendering.
  - `bot/tools/filesystem/search` for glob and grep search tools.
  - `bot/tools/filesystem/edit` for the edit tool, edit preflight/commit/recovery/lint, block resolver, and edit diff/result formatting.
  - `bot/tools/shell` for bash execution.
  - `bot/tools/web` for web search, web fetch, and HTML-to-Markdown conversion.
  - `bot/tools/media` for image generation.
  - `bot/tools/tasktool` for sub-agent task delegation.
  - `bot/tools/todo` for todo list updates.
  - `bot/tools/toolhelpers` for shared tool base types and argument helpers.
  - `bot/tools/editdsl` for the edit patch DSL formerly under `hashline`.
- Updated tool registration to use `tools/catalog.RegisterAll`.
- Updated tests to live with their owning module packages.
- Split `tools/shell/tool_bash.go` into tool definition, danger classification, redirection parsing, and command runner files.
- Split `tools/media/tool_image_gen.go` into tool definition, model/output-dir resolution, Jimeng provider client, and artifact persistence files.
- Moved edit-specific filesystem code into `tools/filesystem/edit` instead of leaving it mixed with read/write/list/tree/glob/grep.
- Moved remaining filesystem tools out of the root filesystem package into `read`, `write`, `list`, `tree`, and `search` submodules.
- Added `tools/filesystem/testutil` for shared filesystem tool test fixtures.
- Split `tools/filesystem/read/tool_read.go` into tool routing, text read/cache formatting, media metadata, and filename suggestion responsibilities.
- Split `tools/filesystem/edit/tool_edit.go` into tool orchestration, preflight/recovery, commit/revert, and lint files.
- Split `tools/filesystem/edit/diff.go` into edit result orchestration, hunk diff rendering, and changed-line/full-file view helpers.
- Updated tool catalog registration and bot tool initialization to import the edit submodule explicitly.
- Added `bot/tools/core` for tool protocol DTOs, tool interface aliases, descriptor-to-LLM conversion, and argument display formatting.
- Added `bot/tools/execution` for per-run execution state, global cache/snapshot fallbacks, file read cache lifecycle, cache transfer, LRU eviction, and read-range merging.
- Added `bot/tools/llmstream` for streamed LLM token consumption, callback accounting, tool-call delta aggregation, and one-shot stream call wrapping.
- Added `bot/tools/runner` for tool execution batching, parallel/sequential scheduling, preview emission, confirmation/plan-mode blocking, panic recovery, output shaping, and cache invalidation.
- Added helper modules `textutil`, `pathutil`, `netclient`, `semantics`, and `snapshots` so generic root utilities now live under named responsibilities.
- Reduced root `tools/types.go`, `tools/file_cache.go`, `tools/streaming.go`, `tools/executor.go`, and `tools/util.go` to compatibility facades over the focused submodules.
- Added direct tests for `tools/core`, `tools/execution`, `tools/llmstream`, `tools/runner`, and the helper modules.

Verification:
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/tools/... ./bot`
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/tools/shell ./bot/tools/...`
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/tools/media ./bot/tools/...`
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/tools/filesystem ./bot/tools/...`
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/tools/filesystem/... ./bot/tools/catalog ./bot`
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/tools/filesystem/... ./bot/tools/catalog ./bot/tools`
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/tools/core ./bot/tools/execution ./bot/tools/llmstream ./bot/tools`
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/tools/runner ./bot/tools/textutil ./bot/tools/pathutil ./bot/tools/netclient ./bot/tools/semantics ./bot/tools ./bot/tools/...`

### Batch 7: Edit DSL Split

Status: completed

Scope:
- Split hashline parsing, payload modeling, normalization, validation, range matching, mismatch reporting, and recovery.
- Replace the giant `hashline_test.go` with responsibility-specific tests.

Completed changes:
- Kept the public DSL package under `bot/tools/editdsl`.
- Split parser types, payload parsing, contamination diagnostics, and range parsing out of `patch.go`.
- Split application result types, after-insert landing repair, boundary repair, delimiter balance repair, block resolution, and blank-line collapse out of `apply.go`.
- Replaced the broad `hashline_test.go` with component-focused test files:
  - `hash_test.go`
  - `parse_test.go`
  - `apply_test.go`
  - `snapshot_test.go`
  - `recovery_test.go`
  - `block_test.go`
  - `integration_test.go`
  - `mapping_test.go`

Verification:
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/tools/editdsl`

### Batch 8: Index Responsibility Split

Status: completed

Scope:
- Split parser, graph, db, and indexer files by the audit table above.
- Add or move tests so each split responsibility has direct coverage.

Completed changes:
- Moved NEKOCODE/project-context discovery out of the root index package into `bot/index/projectctx`.
- Updated bot context initialization to call `projectctx.LoadProjectContext`, leaving root `index` focused on graph/index manager responsibilities.
- Added direct project-context tests for root discovery, rule loading, include expansion, and empty result behavior.
- Moved graph model/query/formatting out of the root index package into `bot/index/graph`.
- Split graph responsibilities into model construction, mutations, lookup/query methods, skeleton formatting, language/module detection, and DTO types.
- Added root `index` facade aliases for parser, DB, graph, indexer, manager, syncer, and project tool APIs; production app wiring now imports the focused subpackages directly where appropriate.
- Moved SQLite persistence out of the root index package into `bot/index/db`.
- Split DB responsibilities into connection lifecycle, schema migration SQL, node/edge repository operations, file hash repository operations, graph loading/clear, and FTS search.
- Added root `index.DB` alias and `index.OpenDB` wrapper as compatibility facade APIs.
- Moved tree-sitter parser implementation out of the root index package into `bot/index/parser`.
- Added root `index.Parser` alias and `index.NewParser` wrapper as compatibility facade APIs.
- Moved index orchestration out of the root index package into `bot/index/indexer`.
- Split indexer responsibilities into lifecycle construction, indexing policy, full-directory walk, stale-cache detection, single-file incremental update, cross-file reference resolution, and FTS search access.
- Updated sync and tool query flows to call explicit indexer methods instead of reaching into parser/DB internals.
- Added root `index.Indexer` alias plus `NewIndexer`, `ShouldSkipDir`, and `SupportsFile` wrappers as compatibility facade APIs.
- Moved project index lifecycle manager into `bot/index/service` and made root `index.Manager` a facade alias.
- Moved filesystem watcher/incremental update syncer into `bot/index/syncer`.
- Added direct syncer tests for supported file create/remove watcher events and unsupported-file filtering.
- Moved `project_info` tool adapter and query formatting into `bot/index/projecttool`.
- Updated app initialization and tool registration to import `index/service` and `index/projecttool` directly, leaving root `index` as compatibility facade.

Verification:
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/index/... ./bot`
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/index/...`
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/index/db ./bot/index/...`
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/index/parser ./bot/index/...`
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/index/indexer ./bot/index/...`
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/index/...`

### Batch 9: Extension Module Consolidation

Status: completed

Scope:
- Move plugin, MCP, and skill systems under `bot/extension`.
- Split plugin registry, MCP client, and skill loader by responsibility before or during the move.

Completed changes:
- Moved plugin implementation from `bot/plugin` to `bot/extension/plugin`; root `bot/plugin` now aliases the extension package API.
- Split plugin responsibilities into manifest parsing, plugin model/resource accessors, recursive resource discovery, registry state persistence, install/copy/git operations, command execution helpers, and registry lifecycle.
- Moved MCP implementation from `bot/mcp` to `bot/extension/mcp`; root `bot/mcp` now aliases the extension package API.
- Split MCP client responsibilities into protocol/client types, process lifecycle, JSON-RPC handshake/request transport, tool list/call RPCs, and tool adapter behavior.
- Moved skill implementation from `bot/skill` to `bot/extension/skill`; root `bot/skill` and `bot/skill/bundled` now alias extension APIs.
- Split skill responsibilities into model, registry/load-state, directory discovery, file loading/auxiliary-file listing, frontmatter parsing, context formatting, and tool adapter execution.
- Updated app, command lifecycle, plugin CLI formatting, and plugin manifest code to import `bot/extension/plugin`, `bot/extension/mcp`, and `bot/extension/skill` directly.

Verification:
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/extension/plugin ./bot/plugin`
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/extension/mcp ./bot/mcp`
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/extension/skill ./bot/extension/skill/bundled ./bot/skill ./bot/skill/bundled`
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/extension/... ./bot/plugin ./bot/mcp ./bot/skill ./bot/skill/bundled ./bot/plugincli`
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/...`

### Batch 10: Context Manager Responsibility Split

Status: completed

Scope:
- Split context manager and memory files by state, build, compaction, persistence, policy, and formatting.
- Add direct tests for manager state transitions and memory responsibilities.

Completed changes:
- Reduced `bot/contextmgr/manager.go` to package documentation, core types, constructors, and summarizer factory.
- Moved context settings and todo state into `settings.go`.
- Moved token usage/cache APIs into `token_usage.go`.
- Moved compaction entry points and archive merge flow into `compaction.go`.
- Moved message length/statistics helpers into `stats.go`.
- Moved build assembly into `build.go`, keeping tool-call validity filtering isolated there.
- Moved history mutation operations into `history.go`.
- Moved session persistence snapshot/restore into `snapshot.go`.
- Split `bot/contextmgr/memory/memory.go` into `load.go`, `save.go`, `build.go`, `append.go`, `merge.go`, `parse.go`, and `fields.go` while keeping the memory module directory intact.

Verification:
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/contextmgr/memory ./bot/contextmgr/...`

### Batch 11: Support Module Coverage And Responsibility Split

Status: completed

Scope:
- Split and test remaining low-coverage support modules: debug logging, prompt building, Volcengine signing, and tree-sitter language registry.
- Keep root package APIs stable through facades where other modules already depend on them.

Completed changes:
- Split debug logging into logger state, file/rotation handling, and caller formatting files; added temp-file tests for log output and rotation.
- Split prompt building into `prompt/system` and `prompt/planmode`; moved embedded markdown into the system prompt module and added deterministic builder tests.
- Split Volcengine Signature V4 support into `sdk/volcengine` signer, canonical request, and crypto helper files; added fixed-clock signature tests.
- Split tree-sitter language registry into `treesitter/languages`; added direct tests for extension coverage and parser construction.

Verification:
- `env GOCACHE=/tmp/nekocode-go-build go test ./bot/debug ./bot/prompt/... ./bot/sdk/... ./bot/treesitter/...`
