# Runtime HTTP API

`runtime/httpapi` exposes the runtime interaction protocol over JSON and
Server-Sent Events. It does not expose bot implementations directly.

## Start the daemon

From the repository root, run:

```bash
go run ./cmd/daemon
```

The server listens on `127.0.0.1:8765` by default. Use `-addr` to change the
listen address. Set `NEKOCODE_DAEMON_TOKEN` or pass `-token` to require
`Authorization: Bearer <token>` on every endpoint except `/health`:

```bash
NEKOCODE_DAEMON_TOKEN=replace-me go run ./cmd/daemon -addr 127.0.0.1:8765
```

The daemon reads the same `~/.nekocode/config.json` model configuration as the
TUI. Build a standalone binary with `go build -o nekocode-daemon ./cmd/daemon`;
static cross-compilation targets are documented in [`build/README.md`](../build/README.md).

## Rules

- JSON requests reject unknown fields and multiple JSON values.
- Runtime protocol errors include a stable `code`.
- SSE event IDs are monotonic cursors. Clients reconnect with `after` or
  `Last-Event-ID`.
- `replay=1` replays retained history before live events.
- `WithBearerAuth` protects every endpoint except `/health`.
- Optional management endpoints return `501 Not Implemented` when their
  capability is absent. `/capabilities` is the authoritative discovery
  response.

## Headless connector bootstrap

The daemon can configure and start IM connectors without a TUI or an HTTP
management client. Select one or more transports with a comma-separated
service variable:

```text
NEKOCODE_CONNECTORS=feishu,telegram,wecom
```

Provide credentials for the selected transports on first boot:

```text
NEKOCODE_FEISHU_APP_ID=cli_xxx
NEKOCODE_FEISHU_APP_SECRET=xxx
NEKOCODE_TELEGRAM_BOT_TOKEN=123456:xxx
NEKOCODE_QQBOT_APP_ID=xxx
NEKOCODE_QQBOT_APP_SECRET=xxx
NEKOCODE_QQBOT_SANDBOX=false
NEKOCODE_WECOM_BOT_ID=xxx
NEKOCODE_WECOM_BOT_SECRET=xxx
```

If `NEKOCODE_CONNECTORS` is omitted, transports are inferred from credential
variables for backward-compatible first boot. Setting it explicitly is
recommended because it also starts connectors from persisted configuration
when credentials are no longer present in the environment. Use `none` to
disable all connector bootstrap.

For Feishu, Telegram, and WeCom, the first boot logs a short-lived pairing
code or link. Complete that action by messaging the bot. Pairing and
credentials are persisted in `~/.nekocode/connect.json`; later daemon restarts
restore the selected connections. Keep the service user's `HOME` stable across
restarts. Changing a Feishu app ID or WeCom bot ID clears its old owner binding
and starts a new pairing flow.

`NEKOCODE_DAEMON_TOKEN` is independent of connector credentials. It protects
the daemon's HTTP management API and is not needed by IM connectors.

## Endpoints

| Method | Path | Description |
| --- | --- | --- |
| GET | `/health` | Process health and protocol version |
| GET | `/status` | Runtime lifecycle and active run |
| GET | `/capabilities` | Optional capability manifest |
| GET | `/observe/overview` | Aggregated observation snapshot for external apps |
| GET | `/observe/runs` | Compact run projections (`state`, `source`, `limit`) |
| GET | `/observe/runs/{runID}` | One run with tool names/status/duration |
| GET | `/observe/metrics` | Prometheus text exposition |
| GET | `/observe/models` | Selectable models and the active one |
| GET | `/observe/tools` | Configured tool catalog (names only) |
| GET | `/observe/sources` | Distinct run source kinds for filter menus |
| POST | `/input` | Start a run |
| GET | `/events?run_id=&after=&replay=1` | Subscribe to runtime events |
| GET | `/runs?limit=50` | List recent run snapshots |
| GET | `/runs/current` | Read the latest run snapshot |
| GET | `/runs/{runID}` | Read one run snapshot |
| POST | `/runs/{runID}/abort` | Cancel the active run |
| POST | `/approvals/{approvalID}` | Resolve a pending approval |
| POST | `/questions/{questionID}` | Answer or reject a pending question |
| GET | `/connect` | Read connector state |
| POST | `/connect/{name}` | Configure or start a connector |
| POST | `/disconnect/{name}` | Stop a connector |
| GET | `/model` | Read the active provider and model |
| GET | `/commands` | Compatibility list derived from the `/` and `$` root menus |
| GET | `/commands/menu?input=/model` | Resolve a transport-neutral command menu |
| GET | `/metrics` | Read the latest operational metrics |
| GET | `/context` | Read the context snapshot |
| GET | `/memory?scope=project` | Read project memory |
| GET | `/sessions` | List sessions |
| GET | `/sessions/current/messages` | Read current session messages |

## Input

`POST /input` accepts:

```json
{
  "text": "hello",
  "source": {"kind": "http", "id": "client-1"},
  "sender": {"id": "user-1"}
}
```

The response is `202 Accepted` with `{"run_id":"run_1"}`. A blank
`source.kind` defaults to `http`.

## Interaction

Approval:

```json
{"allowed":true,"remember":false}
```

When a command has predictable sandbox capabilities, the approval event
contains those details in the same request. One decision covers both; clients
do not need a separate escalation action. Security facts are exposed through
the typed `approval` object (`reason`, `structures`, `capabilities`, `scope`,
`workspace`, `sandbox`, and `write_paths`); `args` contains only the original
tool invocation.
Legacy clients may still send `allow_with_permission`; it is accepted and
ignored because `allowed` now covers the capabilities displayed in the event.

Question:

```json
{"answers":[["choice-a"]],"rejected":false}
```

Connector commands accept an optional body:

```json
{"args":["status"]}
```

## Events

Each SSE item contains the stable event type and the complete versioned event:

```text
id: evt_12
event: run_done
data: {"version":"2.0","id":"evt_12","sequence":12,"run_id":"run_1","type":"run_done","payload":{"output":"done"}}
```

The main lifecycle events are `run_started`, `run_done`, `run_failed`, and
`run_aborted`. Streaming, tool, approval, question, session, connector, and
metrics changes use their corresponding typed runtime events.

## Observation for external applications

`/observe/*` is the transport-neutral surface for anything that is not a
first-party UI: an IM bridge, a web dashboard, a CI job, or a metrics scraper.
It is read-only and requires no frame-protocol parsing.

### What the surface exposes

| Endpoint | Payload | Notes |
|---|---|---|
| `GET /observe/overview` | Aggregated snapshot | Runtime state, capabilities, model, latest metrics, context composition, run counters |
| `GET /observe/runs` | Run list | `?state=`, `?source=`, `?limit=` (max 1000) |
| `GET /observe/runs/{runID}` | One run | Summary + tool names/status/duration + subagent identities |
| `GET /observe/metrics` | Prometheus text | `text/plain; version=0.0.4` |
| `GET /observe/models` | Model catalog | `501` when the runtime has no model catalog |
| `GET /observe/tools` | Tool names | `501` when the runtime has no tool catalog |
| `GET /observe/sources` | Source kinds | For building filter menus |

Every path except `/health` requires the bearer token when one is configured.

### Data boundary

Two limits shape what an observer can see, and both are deliberate:

- **Retention.** Run snapshots live in the runtime's in-memory store, which
  holds the most recent 100 runs (`runstore.DefaultLimit`) in FIFO order. A new
  run evicts the oldest, so the run counters in the overview describe that
  retained window, not the whole session history.
- **Query cap.** `/observe/runs` accepts `limit` up to 1000, but no more rows
  exist than the store retains; the effective ceiling is the retention.

Per-run events are also written to `~/.nekocode/sessions/<session>/runs/<run>/events.jsonl`
and replayed at startup: the newest runs decode synchronously, the rest stream
in from a background goroutine, and the in-memory store keeps the most recent
100. Older runs stay on disk and are not served by `/observe/*`.

### What is deliberately absent

These projections are safe to hand to a third party:

- No prompt text, tool arguments, tool output, or model output.
- No file contents or command lines.
- Tool activity is limited to name, status, start/finish time and duration.
- Todos are exposed as a count, and free-text run errors are omitted. Use the
  authenticated run API for full diagnostic details.

`overview.window` is `"retained"`: the counters cover retained runs, without
claiming a fixed 24-hour time range. Run IDs are opaque strings; new IDs include
a random suffix to keep concurrent processes' recordings separate.

### Metric scope

`Metrics.turn*` fields describe the **most recent** turn. They are not
per-run history and must not be summed. External apps that need per-run token
accounting should read `result.nekocode_usage` from the headless stream or
aggregate the call log. Run counters are gauges named `*_window` because they
move as the retention window slides.

### Stability

The `/observe/*` paths and their JSON field names are the contract. Adding
fields is compatible; the run state vocabulary
(`idle`/`running`/`waiting`/`done`/`failed`/`cancelled`) is fixed.

### Minimal client

```bash
# Start the daemon (no token: loopback only)
go run ./cmd/daemon -addr 127.0.0.1:8765

# Aggregates
curl -s http://127.0.0.1:8765/observe/overview | jq '{runs, metrics, context}'

# Failed runs in the retained window
curl -s 'http://127.0.0.1:8765/observe/runs?state=failed&limit=20' \
  | jq '.runs[] | {id, duration_ms, state}'

# One run's tool timeline
curl -s http://127.0.0.1:8765/observe/runs/run_123 \
  | jq '.tools[] | {name, status, duration_ms}'
```

With a token:

```bash
NEKOCODE_DAEMON_TOKEN=replace-me go run ./cmd/daemon -addr 127.0.0.1:8765
curl -s -H "Authorization: Bearer replace-me" \
  http://127.0.0.1:8765/observe/overview
```

### Browser dashboards (CORS)

Pass `-cors` (or `NEKOCODE_DAEMON_CORS`) a comma-separated list of origins, or
`*`, to enable CORS for GET requests. Preflight is answered before
authentication; credentials are never allowed, so a browser must still send the
token explicitly. Clients that cannot set headers (SSE `EventSource`, plain
navigation) may pass `?token=<token>` instead.

```bash
NEKOCODE_DAEMON_TOKEN=replace-me \
NEKOCODE_DAEMON_CORS=https://dash.example \
go run ./cmd/daemon -addr 127.0.0.1:8765
```

```bash
curl -s -H "Authorization: Bearer replace-me" \
  http://127.0.0.1:8765/observe/overview | jq '.runs, .metrics'
curl -s -H "Authorization: Bearer replace-me" \
  http://127.0.0.1:8765/observe/metrics
```

### Headless hosts

A host that already runs `nekocode-tui --headless` gets the same data without a
daemon. Observation is available as control requests over the frame protocol,
using the same identifiers as the HTTP paths:

| Control subtype | Equivalent to |
|---|---|
| `observe.overview` | `GET /observe/overview` |
| `observe.runs` | `GET /observe/runs` |
| `observe.run` | `GET /observe/runs/{runID}` |
| `context.snapshot` | Live context composition (no HTTP equivalent) |

Request and response envelope:

```json
{"type":"control_request","request_id":"obs-1","request":{"subtype":"observe.runs","limit":20,"state":"failed"}}
{"type":"control_response","response":{"subtype":"success","request_id":"obs-1","response":{"runs":[]}}}
```

`context.snapshot` also reports the active turn's binding and is safe to poll
mid-run. Context samples are cached for up to one second; run and configuration
changes invalidate the cache. Live `context_updated` events reuse those samples
and are emitted only when the snapshot changes, rather than on every metrics
tick. They are transient observations and are not persisted in run recordings:

```json
{"type":"control_request","request_id":"ctx-1","request":{"subtype":"context.snapshot"}}
{"type":"control_response","response":{"subtype":"success","request_id":"ctx-1","response":{"session_id":"s1","run_id":"run_1","user_message_uuid":"u1","context":{}}}}
```

A host must check the advertised capabilities before calling these. The init
frame lists them; observation requires `context.snapshot`, `observe.overview`,
`observe.runs` and `observe.run`, all of which follow the runtime's Context
capability. An unsupported method answers with a `control_response` of subtype
`error`.

For live per-event activity without polling, consume `system/nekocode_event`
frames (`phase_changed`, `todos_updated`, `subagent_*`, `context_updated`) and
`result.nekocode_usage` for per-turn tokens.

### Choosing between the two

| You have | Use |
|---|---|
| A long-lived daemon or an HTTP-capable host | `/observe/*` |
| `nekocode-tui --headless` and a stdio host | `observe.*` control requests |
| One in-flight turn to render live | `context.snapshot` + `system/nekocode_event` |

Both paths are backed by the same runtime projections, so an application that
mixes them sees identical numbers.
