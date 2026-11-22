"""Explicit adapter for current NekoCode session journals and physical call logs.

Not the public protocol: missing/rotated data is reported, never replaced by zero.
"""
import json
import os
import sqlite3
import stat
import time
from pathlib import Path


class Telemetry:
    def __init__(self, root, session=None):
        self.root = Path(root)
        self.session = session
        self.calls = {}
        self.read_errors = {}
        self._events = None
        self._files = {}
        self._bound_session = session

    def close(self):
        if self._events is not None:
            self._events.close()
            self._events = None

    def event_rows(self, *types):
        if self._events is not None:
            query = "SELECT row FROM events"
            if types:
                query += " WHERE kind IN (" + ",".join("?" for _ in types) + ")"
            for row, in self._events.execute(query + " ORDER BY rowid", types):
                yield json.loads(row)

    def _rows(self, path, deadline=None):
        # Resume at the last complete line. Detect replacement/rotation and
        # copytruncate, including truncate-and-regrow past the old offset.
        try:
            if not stat.S_ISREG(path.stat().st_mode):
                self.read_errors[str(path.relative_to(self.root))] = "NotRegularFile"
                return
            with path.open('rb') as stream:
                info = os.fstat(stream.fileno())
                identity = (info.st_dev, info.st_ino)
                offset, anchor, previous = self._files.get(path, (0, b'', None))
                if previous != identity or info.st_size < offset:
                    offset, anchor = 0, b''
                if anchor:
                    stream.seek(offset - len(anchor))
                    if stream.read(len(anchor)) != anchor:
                        offset, anchor = 0, b''
                stream.seek(offset)
                # One poll observes a fixed prefix even while the journal grows.
                while offset < info.st_size and (deadline is None or time.monotonic() < deadline):
                    line = stream.readline(info.st_size - offset)
                    if not line or not line.endswith(b'\n'):
                        break
                    offset = stream.tell()
                    try:
                        row = json.loads(line)
                    except (ValueError, UnicodeError):
                        continue
                    if isinstance(row, dict):
                        yield row
                stream.seek(max(0, offset - 64))
                anchor = stream.read(min(64, offset))
                self._files[path] = (offset, anchor, identity)
            self.read_errors.pop(str(path.relative_to(self.root)), None)
        except FileNotFoundError:
            # A writer can rotate a journal between glob and open.
            self._files.pop(path, None)
        except OSError as exc:
            self.read_errors[str(path.relative_to(self.root))] = type(exc).__name__

    def refresh(self, *, live=False):
        if not self.session:
            return
        if self._bound_session != self.session:
            self.calls.clear()
            self.read_errors.clear()
            self.close()
            self._files.clear()
            self._bound_session = self.session
        # Return control to the protocol loop for approvals and budget checks.
        # After process shutdown the final refresh drains all remaining rows.
        deadline = time.monotonic() + 0.1 if live else None
        active = set()
        for path in (self.root / "logs").glob("llm-calls.jsonl*"):
            active.add(path)
            for row in self._rows(path, deadline):
                sid = row.get("session", "")
                if not isinstance(sid, str) or (sid != self.session and not sid.startswith(self.session + "/")):
                    continue
                key = row.get("call_id") or (row.get("ts"), row.get("seq"), row.get("source"), sid)
                self.calls[str(key)] = row
        directory = self.root / "sessions" / self.session / "runs"
        for path in directory.glob("*/events.jsonl"):
            active.add(path)
            for row in self._rows(path, deadline):
                key = (row.get("run_id"), row.get("id") or row.get("sequence"))
                if self._events is None:
                    # An unnamed on-disk SQLite database is removed on close.
                    # Keep the first-seen order and the latest row across rotation.
                    self._events = sqlite3.connect("")
                    self._events.execute("CREATE TABLE events (id TEXT UNIQUE, kind TEXT, row TEXT)")
                self._events.execute(
                    "INSERT INTO events VALUES (?, ?, ?) ON CONFLICT(id) DO UPDATE "
                    "SET kind=excluded.kind, row=excluded.row",
                    (str(key), row.get("type") if isinstance(row.get("type"), str) else None,
                     json.dumps(row, separators=(",", ":"))))
        if self._events is not None:
            self._events.commit()
        self._files = {p: state for p, state in self._files.items() if p in active}

    def background(self):
        if not self.session:
            return None
        path = self.root / "sessions" / self.session / "background.json"
        try:
            state = json.loads(path.read_text())
            if not isinstance(state, dict) or state.get("version") not in {1, 2} or not isinstance(state.get("tasks"), list):
                return None
            if any(not isinstance(t, dict) or t.get("session_id") != self.session or not isinstance(t.get("status"), str) for t in state["tasks"]):
                return None
            return state
        except (OSError, ValueError):
            return None

    def model_cost(self):
        rows = self.calls.values()
        def usage_known(row):
            # New records are explicit. Older records without the flag can be
            # trusted only when they contain a non-zero provider usage value.
            if row.get("usage_reported") is not None:
                return row.get("usage_reported") is True
            return any(type(row.get(key)) is int and row[key] > 0 for key in ("in", "out", "total"))
        def total(key):
            return sum(r[key] for r in rows) if rows and all(type(r.get(key)) is int and usage_known(r) for r in rows) else None
        return {"model_calls": len(rows) if rows else None,
                "tool_model_calls": sum(r.get("source") == "image_understand" for r in rows) if rows else None,
                "usage_unknown_calls": sum(not usage_known(r) for r in rows) if rows else None,
                "observed_total_tokens": sum(r["total"] for r in rows if type(r.get("total")) is int and usage_known(r)) if rows else None, "input_tokens": total("in"),
                "output_tokens": total("out"), "total_tokens": total("total"),
                "fresh_tokens": total("new") if rows and all(r.get("cache_usage_reported") for r in rows) else None,
                "cached_tokens": total("cached") if rows and all(r.get("cache_usage_reported") for r in rows) else None}

    def cost(self):
        counts = None
        if self._events is not None:
            counts = dict(self._events.execute("SELECT kind, COUNT(*) FROM events GROUP BY kind"))
        started = counts.get("tool_started", 0) if counts else None
        return {**self.model_cost(), "tool_started": started, "tool_calls": started,
                "tool_blocked": counts.get("tool_blocked", 0) if counts else None}

    def safe_calls(self):
        keys = {"call_id", "tool_call_id", "usage_reported", "failure_stage", "ts", "seq", "source", "session", "model", "execution", "requested_effort", "effective_effort", "dur_ms", "ttft_ms", "in", "out", "total", "new", "cached", "reasoning", "cache_usage_reported", "err"}
        return [{k: v for k, v in r.items() if k in keys} for r in self.calls.values()]


def protocol_rows(path):
    with Path(path).open() as stream:
        for line in stream:
            yield json.loads(line)


def protocol_tool_cost(path, session):
    """Count root task wire requests/results, separately from runtime starts.

    Missing traces are unknown. Child-agent and setup frames are excluded;
    tool-internal model calls and process exit status are outside this metric.
    """
    unknown = {"tool_rounds": None, "tool_rounds_before_first_read": None, "repeated_tool_requests": None, "tool_requests": None, "tool_result_errors": None, "context_snapshot_polls": None, "same_image_rechecks": None, "image_rate_limits": None}
    if not session:
        return unknown
    requests, errors = set(), set()
    signatures = set()
    # The root runtime emits every tool start in a batch before its results.
    # Headless assigns a separate assistant message ID to each start.
    active_batch = set()
    batch_run = None
    batch_count = 0
    read_rounds = {}
    successful_read_round = None
    repeated = 0
    signatures_complete = True
    image_requests, image_limits = set(), set()
    approval_digests = {}
    context_snapshot_polls = 0
    observed = False
    try:
        for row in protocol_rows(path):
            frame = row.get("message", {})
            # Permission frames carry a digest of the ordered approved image bytes.
            # Some policies produce no approval frame; keep that measurement unknown.
            if row.get("direction") == "agent" and frame.get("type") == "control_request":
                req = frame.get("request", {})
                if req.get("tool_name") == "image_understand" and not req.get("nekocode_subagent_id"):
                    digest = req.get("input", {}).get("_nekocode_approved_image_sha256", "")
                    if isinstance(digest, str) and len(digest) == 64:
                        approval_digests[req.get("tool_use_id")] = digest
            if (row.get("direction") == "host" and frame.get("type") == "control_request"
                    and frame.get("request", {}).get("subtype") == "context.snapshot"):
                context_snapshot_polls += 1
            if (row.get("direction") != "agent" or frame.get("session_id") != session
                    or frame.get("parent_tool_use_id") is not None
                    or frame.get("nekocode_subagent_id")
                    or frame.get("user_message_uuid") != "task"):
                continue
            observed = True
            if frame.get("type") not in {"assistant", "user"}:
                continue
            content = frame.get("message", {}).get("content", [])
            if not isinstance(content, list):
                continue
            for block in content:
                if not isinstance(block, dict):
                    continue
                if frame.get("type") == "assistant" and block.get("type") == "tool_use" and block.get("id"):
                    identity = (frame.get("run_id"), block["id"])
                    if identity in requests:
                        continue
                    requests.add(identity)
                    if not active_batch or batch_run != identity[0]:
                        active_batch.clear()
                        batch_run = identity[0]
                        batch_count += 1
                    active_batch.add(identity)
                    if not block.get("name") or not isinstance(block.get("input"), dict):
                        signatures_complete = False
                        continue
                    if block["name"] == "image_understand":
                        image_requests.add(block["id"])
                    if block["name"] == "read":
                        read_rounds[identity] = batch_count - 1
                    signature = (identity[0], block["name"], json.dumps(block["input"], sort_keys=True))
                    if signature in signatures:
                        repeated += 1
                    signatures.add(signature)
                if frame.get("type") == "user" and block.get("type") == "tool_result":
                    active_batch.discard((frame.get("run_id"), block.get("tool_use_id")))
                if frame.get("type") == "user" and block.get("type") == "tool_result" and not block.get("is_error"):
                    read_round = read_rounds.get((frame.get("run_id"), block.get("tool_use_id")))
                    if read_round is not None and successful_read_round is None:
                        successful_read_round = read_round
                if (frame.get("type") == "user" and block.get("type") == "tool_result"
                        and block.get("is_error") is True and block.get("tool_use_id")):
                    errors.add((frame.get("run_id"), block["tool_use_id"]))
                    if block["tool_use_id"] in image_requests and "HTTP 429" in str(block.get("content", "")):
                        image_limits.add(block["tool_use_id"])
    except (OSError, ValueError, TypeError, AttributeError):
        return unknown
    return {"tool_rounds": batch_count, "tool_rounds_before_first_read": successful_read_round, "repeated_tool_requests": repeated if signatures_complete else None, "tool_requests": len(requests), "tool_result_errors": len(errors),
            "same_image_rechecks": len(image_requests) - len({approval_digests[k] for k in image_requests}) if image_requests <= approval_digests.keys() else None,
            "image_rate_limits": len(image_limits), "context_snapshot_polls": context_snapshot_polls} if observed else unknown


def coordination_audit(events, background):
    """Observable workflow quality, never a semantic correctness score."""
    observed = False
    errors = result_errors = 0
    for event in events:
        observed = True
        payload = event.get('payload')
        if event.get('type') == 'tool_completed' and isinstance(payload, dict) and payload.get('is_error'):
            errors += 1
            result_errors += payload.get('tool_name') == 'task_result'
    tasks = background.get('tasks', []) if isinstance(background, dict) else None
    audit = {
        'schema_version': 2,
        'all_runtime_tool_errors': errors if observed else None,
        'task_result_read_errors': result_errors if observed else None,
        'handoff_outcomes': {state: sum((t.get('review') or {}).get('outcome', 'legacy') == state for t in tasks)
                             for state in ['complete', 'continue', 'blocked', 'decision', 'legacy']} if tasks is not None else None,
        'limitations': ['Scheduling outcomes are worker reports, not independently verified task quality.',
                        'Runtime completed-tool errors include observed coordinator/worker events; missing/rotated journals can undercount.']}
    return audit
