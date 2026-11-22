"""Check foreground task lifecycles in the headless trial's runtime journal."""
import json
from pathlib import Path
import shlex

CHECK_IDS = ("workers", "parallelism", "integration_run")
WORK_TOOLS = {"read", "write", "edit", "grep", "glob", "diff", "index", "lsp_definition",
              "lsp_references", "lsp_hover", "lsp_diagnostics", "shell"}
TEST_COMMAND = ["python3", "-m", "unittest", "discover", "-s", "tests"]


def exited_ok(process):
    # Go omits exit_code when its numeric value is zero.
    return (process.get("state") == "exited" and process.get("exit_code_known") is True
            and process.get("exit_code", 0) == 0)


def matches_call(item, run, payload):
    identity = item.get("execution") or {}
    return (item.get("id") and item.get("tool") == payload.get("tool_name")
            and item.get("call_id") and item["call_id"] == payload.get("call_id")
            and identity.get("run_id") == run
            and identity.get("task_id", "") == payload.get("subagent_id", "")
            and identity.get("attempt_id", "") == payload.get("attempt_id", ""))


def receipt(run, payload):
    evidence = payload.get("evidence") or {}
    if payload.get("is_error") or payload.get("non_execution") or evidence.get("preservation_failed"):
        return {}
    for item in evidence.get("executions", []):
        # A live process has no exit verdict yet. Keep its launch receipt so
        # a later successful wait can establish completion for this handle.
        running = (item.get("status") == "unknown" and item.get("tool") in ("shell", "process")
                   and (item.get("process") or {}).get("state") == "running")
        if (item.get("status") == "success" or running) and matches_call(item, run, payload):
            return item
    return {}


def test_command(command):
    if not isinstance(command, str):
        return False
    # shlex does not implement the shell's backslash-newline removal.
    # Preserve quoted literals and escaped backslashes for shlex to decode.
    chars = iter(command)
    normalized = []
    quote = None
    for char in chars:
        if char == "\\" and quote != "'":
            following = next(chars, "")
            if following != "\n":
                normalized.extend((char, following))
            continue
        if char in ("'", '"'):
            if quote is None:
                quote = char
            elif quote == char:
                quote = None
        normalized.append(char)
    command = "".join(normalized).strip(" \t\n")
    if "\n" in command or "\r" in command:
        return False
    try:
        args = shlex.split(command)
    except ValueError:
        return False
    return args in (TEST_COMMAND, TEST_COMMAND + ["-v"])


def projected_events(rows):
    # Telemetry collects multiple run journals by polling. Retain small fields
    # and use the global event sequence rather than file order or wall time.
    roots, events = set(), []
    queue_transitions = {}
    unexecuted = set()
    for row in rows:
        kind, run = row.get("type"), row.get("run_id")
        source = row.get("source") or {}
        if kind == "run_started" and source == {"kind": "headless", "id": "task"} and run:
            roots.add(run)
        seq = row.get("sequence")
        if type(seq) is not int or seq < 1:
            continue
        p = row.get("payload") or {}
        if kind == "interaction_queue" and source.get("kind") == "runtime" and p.get("subagent_id") and p.get("id"):
            waiting = p.get("waiting")
            if type(waiting) is not bool or type(p["id"]) is not int or not isinstance(p["subagent_id"], str):
                raise ValueError("invalid interaction queue event")
            # Older journals may repeat a release millions of times. Only the
            # first transition in sequence order changes the worker's interval.
            key = (run, p["subagent_id"], p["id"], waiting)
            queue_transitions[key] = min(seq, queue_transitions.get(key, seq))
        if source.get("kind") == "runtime" and kind in (
                "approval_requested", "approval_resolved", "question_requested", "question_resolved"):
            if p.get("subagent_id") and p.get("id"):
                family, action = kind.split("_")
                events.append((seq, run, action, p["subagent_id"], None, (family, p["id"])))
        if source.get("kind") != "bot":
            continue
        if kind in ("subagent_started", "subagent_ended", "subagent_updated"):
            events.append((seq, run, kind, p.get("id"), p.get("attempt_id"), p.get("status")))
        elif kind in ("tool_started", "tool_completed"):
            name, child, call = p.get("tool_name"), p.get("subagent_id"), p.get("call_id")
            args = p.get("input")
            if not isinstance(args, dict):
                args = {}
            action = args.get("action")
            watch = name == "process" and isinstance(action, str) and action.strip().lower() in ("wait", "watch")
            ref = args.get("task")
            watch_task = ref.strip() if watch and isinstance(ref, str) else None
            if child:
                if kind != "tool_completed" or not p.get("attempt_id") or (name not in WORK_TOOLS and not watch):
                    continue
                item = receipt(run, p)
                if not item:
                    continue
                proc = item.get("process") or {}
                task = proc.get("task")
                if name == "shell" and task and proc.get("state") == "running" and proc.get("reason") != "reused_running":
                    events.append((seq, run, "shell_running", child, p["attempt_id"], task))
                elif watch:
                    if task and watch_task == task and exited_ok(proc):
                        events.append((seq, run, "shell_finished", child, p["attempt_id"], task))
                elif name != "shell" or exited_ok(proc):
                    events.append((seq, run, "work", child, p["attempt_id"], None))
            elif call:
                # Drop both lifecycle events only when the runtime confirms this
                # exact call never executed. An unfinished write still counts.
                if (kind == "tool_completed" and matches_call(p.get("non_execution") or {}, run, p)
                        and not (p.get("evidence") or {}).get("executions")):
                    unexecuted.add((run, call))
                    continue
                command = args.get("command", "")
                is_test = name == "shell" and test_command(command)
                inspection = (args.get("sandbox_mode") == "read-only" or
                              command in ("git status", "git status --short", "git diff --check"))
                invalidates_test = name in ("write", "edit", "task") or is_test
                unknown_effect = name == "shell" and isinstance(command, str) and not is_test and not inspection
                item = receipt(run, p) if kind == "tool_completed" and (is_test or watch) else {}
                # Process metadata is authoritative even when the textual output was truncated.
                events.append((seq, run, kind, call, (invalidates_test, unknown_effect, is_test, watch_task),
                               {**item.get("process", {}), "ended_at": item.get("ended_at", 0)}))
    if len(roots) != 1:
        raise ValueError(f"expected one headless task run, found {len(roots)}")
    root = roots.pop()
    for (run, child, queue_id, waiting), seq in queue_transitions.items():
        if run == root:
            events.append((seq, run, "requested" if waiting else "resolved", child, None, ("queue", queue_id)))
    return sorted((e for e in events if e[1] == root
                   and not (e[2] == "tool_started" and (root, e[3]) in unexecuted)), key=lambda e: e[0])


def workers(events):
    # Retries share a child ID. Keep a running interval only when that
    # attempt eventually completed and had its own successful work receipt.
    active, delivered, intervals = {}, set(), []
    last_completed = 0
    for seq, _, kind, child, attempt, status in events:
        if not child or kind not in ("subagent_started", "subagent_updated", "subagent_ended", "work", "shell_running", "shell_finished", "requested", "resolved"):
            continue
        if kind == "subagent_started" and status == "running":
            active[child] = {"attempt": attempt, "start": seq, "intervals": [], "worked": False, "waiting": False, "pending": set(), "processes": set()}
            continue
        current = active.get(child)
        if not current or (current["attempt"] and attempt and current["attempt"] != attempt):
            continue
        if attempt:
            current["attempt"] = attempt
        if kind == "work":
            current["worked"] = True
        elif kind == "shell_running":
            current["processes"].add(status)
        elif kind == "shell_finished" and status in current["processes"]:
            current["worked"] = True
        elif kind == "requested":
            current["pending"].add(status)
        elif kind == "resolved":
            current["pending"].discard(status)
        elif kind == "subagent_updated" and status in ("running", "waiting_approval", "waiting_question"):
            current["waiting"] = status != "running"
        running = kind != "subagent_ended" and not current["waiting"] and not current["pending"]
        if running and current["start"] is None:
            current["start"] = seq
        elif not running and current["start"] is not None:
            current["intervals"].append((current["start"], seq))
            current["start"] = None
        if kind == "subagent_ended":
            del active[child]
            if status == "completed":
                last_completed = seq
                if attempt and current["worked"]:
                    delivered.add(child)
                    intervals.extend(current["intervals"])
    concurrent = peak = 0
    for _, delta in sorted((seq, delta) for start, end in intervals for seq, delta in ((start, 1), (end, -1))):
        concurrent += delta
        peak = max(peak, concurrent)
    return len(delivered), peak, last_completed


def final_test(events, after, workspace_change_ns):
    if not after:
        return False
    inflight, batched = {}, set()
    process_task, passed = None, False
    passed_at = 0
    for seq, _, kind, call, detail, proc in events:
        if kind not in ("tool_started", "tool_completed"):
            continue
        invalidates_test, unknown_effect, is_test, watch_task = detail
        if kind == "tool_started":
            if inflight:
                batched.update((*inflight, call))
            inflight[call] = seq
            # Reading an artifact in a writable sandbox does not change the
            # tested tree. Directory ctimes also cover removals and renames.
            unchanged = passed and workspace_change_ns is not None and 0 < workspace_change_ns < passed_at
            if invalidates_test or (unknown_effect and not unchanged):
                passed, process_task = False, None
            continue
        start = inflight.pop(call, 0)
        alone = call not in batched
        batched.discard(call)
        if start <= after or not alone:
            continue
        if is_test and proc.get("reason") != "reused_running":
            passed = exited_ok(proc)
            passed_at = proc["ended_at"]
            process_task = proc.get("task") if proc.get("state") == "running" else None
        elif process_task and watch_task == process_task and proc.get("task") == process_task:
            passed = exited_ok(proc)
            passed_at = proc["ended_at"]
            if proc.get("state") != "running":
                process_task = None
    return passed


def collaboration_checks(trace_path, workspace_change_ns=None):
    def unavailable(status, reason):
        return [dict(id=key, status=status, evidence=reason) for key in CHECK_IDS]
    if not trace_path or not Path(trace_path).is_file():
        return unavailable("not_run", "Runtime event journal unavailable")
    try:
        with Path(trace_path).open(encoding="utf-8") as stream:
            events = projected_events(json.loads(line) for line in stream if line.strip())
    except (OSError, UnicodeError, ValueError) as exc:
        return unavailable("failed", f"Unusable runtime journal: {exc}")
    count, peak, last_completed = workers(events)
    checked = final_test(events, last_completed, workspace_change_ns)
    return [dict(id=key, status="passed" if result else "failed", evidence=detail) for key, result, detail in (
        ("workers", count >= 12, f"{count} distinct completed children with per-attempt work receipts; required 12"),
        ("parallelism", peak >= 8, f"Peak overlapping running intervals of those children: {peak}; required 8"),
        ("integration_run", checked, f"Parent unittest discovery exited 0 after the final completed child: {checked}"),
    )]
