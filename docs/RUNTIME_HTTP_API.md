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
