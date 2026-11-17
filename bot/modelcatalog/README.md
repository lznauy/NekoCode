# Public model limits

The embedded `snapshot.json` is generated from [Models.dev](https://models.dev/)'s
public provider catalog, with its MIT license retained in `snapshot.LICENSE`.
It contains text-output model limits, reasoning capabilities and API endpoints
used for matching.
Those endpoints never change connection routing or receive credentials.

Refresh the bundled data from the repository root:

```bash
go run ./scripts/update-model-catalog
```

Or regenerate without networking from a previously downloaded provider catalog:

```bash
go run ./scripts/update-model-catalog -input /path/to/models-dev.json
```

Real GUI, TUI, headless and daemon executables check for a cache refresh in the
background at startup, at most once per 24 hours per fresh cache. The fixed
request is `GET https://models.dev/api.json`; no model credentials, configured
model IDs, or conversation content are included. Requests time out after ten
seconds and reject redirects outside the same HTTPS host. Only a validated
response replaces `~/.nekocode/cache/models-dev.json`, by atomic rename.
Set `NEKOCODE_MODEL_CATALOG_OFFLINE=1` to disable remote updates; cached and
bundled data remain usable. Library constructors and config getters do not
start network requests.

Each loaded configuration owns an immutable catalog snapshot. Refreshing the
cache affects future configurations, normally on the next application start;
it does not alter context budgets during a conversation. UI previews and the
compaction model use that same snapshot. Explicit `context_window` overrides
win; an exact API endpoint and model ID match comes next, then the legacy
compatibility profiles and final default. API roots with and without one
trailing `/v1` are accepted, with exact paths preferred. Provider display names,
model-family guesses and arbitrary prefix stripping are not used for public
catalog matching. Conflicting records for the same endpoint and model are
omitted. Invalid, missing or unreachable data leaves offline fallback intact.

A separate input ceiling, when reported, conservatively constrains the context
budget as well. Output limits are retained but do not automatically raise
generation budgets. Native vision sending, reasoning wire parameters,
API routing and pricing remain under their existing configuration/policy.

Reasoning metadata is stored in version 2 snapshots. Older cache schemas remain
readable for migration but do not override the new bundled snapshot; the next
background refresh replaces them, even within the usual 24-hour freshness
period. `reasoning_options` supplies exact effort lists, while toggle-only and
budget-only capabilities do not invent effort levels. A supported local wire
toggle may still expose `none` when the catalog explicitly advertises a toggle.
Unknown or malformed option fields fall back to local capability profiles;
explicit `reasoning=false` provides only `auto`. `null` and `default` effort
values mean `auto`, not additional UI choices. Returned metadata is copied, so
callers cannot mutate a loaded catalog. Protocol-specific thinking fields and
reasoning replay policies remain local; remote body/header fragments are never
applied. Explicit efforts are validated against the same snapshot used by the
menu and actual requests.

Version 3 adds a separate origin-reasoning index for unlisted relays. Only
provider records whose provider ID equals the author in `canonical_model_id`
can populate it; ordinary reseller records cannot. IDs must match exactly,
without case folding, punctuation removal, prefix stripping or suffix removal.
Conflicting author records are omitted. Current-service reasoning metadata,
including an explicit unsupported result, always wins. Missing service
reasoning metadata may use the origin's same-ID capability as an unconfirmed
reference. This index has no token limits or routing data, so it cannot change
context budgets, generation limits, URLs or credentials. Menus and the GUI
label the reference source and warn that the current service is unconfirmed.
Caches from versions 1/2 fall back to the bundled snapshot until refreshed;
running configurations keep their original snapshot. Service rejections remain
errors; the fallback never authorizes replaying the request with another effort.
