from contextlib import closing
import json
import os
from pathlib import Path
import platform
import queue
import subprocess
import tempfile
import threading
import time

from .storage import copy_tree, digest, tree_hash, workspace_change_ns, write_json, evaluator_fingerprint, json_hash, EVALUATION_CONTRACT_VERSION
from .telemetry import coordination_audit, Telemetry, protocol_tool_cost
from . import verifier_sandbox
from .verification import verify, stop_process
from .layout import TrialLayout
from .repository import prepare_repository, run_git

CONTEXT_SNAPSHOT_INTERVAL = 3
MAX_CONTEXT_SNAPSHOT_INTERVAL = 12
BUDGET_CHECK_INTERVAL = 1


def environment_receipt():
    return {"system": platform.system(), "machine": platform.machine(), "python": platform.python_version(),
            "node": version("node", "--version"), "chrome": version(os.environ.get("CHROME_BIN", "google-chrome"), "--version"),
            "playwright_core": version("node", "-e", "console.log(require('playwright-core/package.json').version)"),
            "node_path": os.environ.get("NODE_PATH"), "locale": os.environ.get("LANG")}


def version(*args):
    try:
        return subprocess.check_output(args, stderr=subprocess.DEVNULL, timeout=5).decode().strip()
    except (OSError, subprocess.SubprocessError):
        return None


def model_observation(selection, calls):
    # Avoiding a worker/compaction is a valid harness improvement. Compare
    # primary model identities without requiring identical actor inventories.
    identity = lambda c: (c.get("model") or "", c.get("requested_effort") or "", c.get("effective_effort") or "")
    primary = sorted({identity(c) for c in calls if c.get("source") in {"main", "subagent"}})
    auxiliary = {}
    for call in calls:
        if call.get("source") not in {"main", "subagent"}:
            auxiliary.setdefault(call.get("source", "unknown"), set()).add(identity(call))
    auxiliary = {k: sorted(v) for k, v in auxiliary.items()}
    known = primary and selection and all(isinstance(c.get("model"), str) and c["model"].strip() for c in calls)
    return (json_hash({"selection": selection, "primary": primary}) if known else None), auxiliary


def run_trial(case, binary, model, mode, telemetry_root, directory, repeat, policy="deny", config_fingerprint=None, parallel_step_sharing=False, no_execution_budget=False):
    with tempfile.TemporaryDirectory(prefix='nekocode-eval-work-') as temporary, closing(Telemetry(telemetry_root)) as telemetry:
        args = (case, binary, model, mode, telemetry, directory, repeat, policy, config_fingerprint)
        return _run_trial(*args, workspace=Path(temporary) / 'workspace', parallel_step_sharing=parallel_step_sharing, no_execution_budget=no_execution_budget)


def _preparation_failed(case, directory, receipt, telemetry, started, exc):
    error = f'{type(exc).__name__}: {exc}'
    if not (directory / 'trace/preparation.log').exists():
        (directory / 'trace/preparation.log').write_text(error + '\n')
    receipt['preparation_elapsed_s'] = round(time.monotonic() - started, 3)
    write_json(directory / 'receipt.json', receipt)
    verification = {'schema_version': 1, 'error': 'input preparation failed', 'required_passed': 0,
                    'required_total': sum(r['required'] for r in case['manifest']['requirements']),
                    'product_passed': False, 'checks': [{'id': r['id'], 'status': 'not_run', 'evidence': error}
                    for r in case['manifest']['requirements']]}
    cost = telemetry.cost()
    cost.update(protocol_tool_cost(directory / 'trace/protocol.jsonl', None))
    outcome = {'schema_version': 1, 'layout_version': 2, 'trial_id': directory.name,
               'stop_reason': 'preparation_failed', 'error': error, 'elapsed_s': receipt['preparation_elapsed_s'],
               'verification_elapsed_s': 0, 'cost': cost, 'verification': verification,
               'success': False, 'product_retained': False, 'product_sha256': None,
               'integrity': {'calls_observed': False, 'all_call_ids_present': False},
               'public_initial_answer_present': False, 'public_final_answer_present': False}
    write_json(directory / 'outcome.json', outcome)
    write_json(directory / 'state.json', {'status': 'finished'})
    (directory / 'answer.txt').write_text('')
    (directory / 'report.md').write_text(f"# {case['manifest']['title']} / {directory.name}\n\nInput preparation failed before the Agent started.\n\n{error}\n\nSee [preparation log](trace/preparation.log) and [receipt](receipt.json).\n")
    return outcome


def execute_agent(binary, workspace, model, mode, policy, prompt, receipt, telemetry, layout):
    """Drive the public protocol and collect execution evidence before verification."""
    trace = layout.trace("protocol.jsonl").open("w")
    errors = layout.trace("stderr.log").open("w")
    inbox = queue.Queue(maxsize=2048)
    trace_lock = threading.Lock()
    reader_error = []
    def record(direction, message):
        with trace_lock:
            trace.write(json.dumps({"elapsed_s": round(time.monotonic() - started, 4), "direction": direction, "message": message}, ensure_ascii=False) + "\n")
            trace.flush()
    started = time.monotonic()
    proc = None
    final_text, final_visible, task_result_seen = "", False, False
    coordinator_turns = set()
    stop, failure = "harness_error", None
    server_info = None
    model_selection = None
    config_observed = None
    interactions = {"allowed": 0, "denied": 0, "questions_rejected": 0, "host_requests": 0}
    last_poll, last_budget_check, pending_poll, quiet_since = 0, 0, False, None
    poll_interval = CONTEXT_SNAPSHOT_INTERVAL
    task_sent = False
    native_budget = None
    mode_ok = False
    def send(frame):
        record("host", frame)
        proc.stdin.write(json.dumps(frame, ensure_ascii=False) + "\n")
        proc.stdin.flush()
    def user(uid, text):
        send({"type": "user", "uuid": uid, "message": {"role": "user", "content": text}})
    def control(uid, subtype, **fields):
        send({"type": "control_request", "request_id": uid, "request": {"subtype": subtype, **fields}})
    try:
        proc = subprocess.Popen([str(Path(binary).resolve()), "--headless"], cwd=workspace, stdin=subprocess.PIPE,
                                stdout=subprocess.PIPE, stderr=errors, text=True, bufsize=1, start_new_session=True)
        def reader():
            try:
                for line in proc.stdout:
                    frame = json.loads(line)
                    if not isinstance(frame, dict):
                        raise ValueError("protocol frame must be an object")
                    record("agent", frame)
                    inbox.put(frame, timeout=2)
            except (ValueError, OSError, queue.Full) as exc:
                reader_error.append(str(exc))
            finally:
                try:
                    inbox.put(None, timeout=2)
                except queue.Full:
                    pass
        thread = threading.Thread(target=reader, daemon=True)
        thread.start()
        budgets = receipt["budgets"]
        options = {}
        if budgets["model_calls"] is not None:
            options.update(model_call_budget=budgets["model_calls"], external_token_limit=budgets["total_tokens"])
        if receipt.get("parallel_step_sharing"):
            options["parallel_step_sharing"] = True
        control("initialize", "initialize", **options)
        user("setup-mode", "/mode " + mode)
        while True:
            telemetry_updated = False
            elapsed = time.monotonic() - started
            if budgets["wall_seconds"] is not None and elapsed >= budgets["wall_seconds"]:
                stop = "wall_budget"
                break
            if reader_error:
                failure = "protocol reader: " + reader_error[0]
                break
            try:
                frame = inbox.get(timeout=.25)
            except queue.Empty:
                frame = {}
            if frame is None:
                stop = "harness_error"
                failure = "headless exited before a settled task"
                break
            if frame.get("session_id"):
                sid = frame["session_id"]
                if telemetry.session and sid != telemetry.session:
                    raise RuntimeError("session binding changed during trial")
                if "/" in sid or "\\" in sid or sid in {".", ".."}:
                    raise RuntimeError("invalid session ID")
                telemetry.session = sid
            if frame.get("type") == "system" and frame.get("subtype") == "error" and frame.get("fatal"):
                failure = frame.get("error", "fatal protocol error")
                break
            if frame.get("subtype") == "message" and frame.get("message") == "Mode: " + mode:
                mode_ok = True
            if frame.get("type") == "result" and frame.get("user_message_uuid") == "setup-mode":
                if not mode_ok or frame.get("is_error"):
                    raise RuntimeError("mode setup failed")
                control("setup-model", "model.set", model=model)
            if (frame.get("type") == "system" and frame.get("subtype") == "init" and
                    frame.get("nekocode_source", {}).get("kind") == "task_completion" and
                    frame.get("user_message_uuid")):
                coordinator_turns.add(frame["user_message_uuid"])
            if frame.get("type") == "control_response":
                response = frame.get("response", {})
                uid = response.get("request_id")
                if uid in {"initialize", "setup-model", "setup-info", "setup-extensions"} and response.get("subtype") != "success":
                    raise RuntimeError(f"{uid} failed: {response.get('error')}")
                if uid == "setup-model":
                    model_selection = response.get("response")
                    control("setup-info", "server.info")
                elif uid == "setup-info":
                    server_info = response.get("response", {})
                    native_budget = server_info.get("execution_budget")
                    if receipt.get("parallel_step_sharing") and not (native_budget or {}).get("parallel_step_sharing"):
                        raise RuntimeError("binary did not enable requested parallel step sharing")
                    control("setup-extensions", "extensions.list")
                elif uid == "setup-extensions":
                    config_observed = response.get("response")
                    user("task", prompt)
                    task_sent = True
                elif uid == "poll":
                    pending_poll = False
                    if response.get("subtype") != "success":
                        raise RuntimeError("context.snapshot unavailable; refusing to guess settlement")
                    view = response.get("response", {})
                    native_budget = view.get("execution_budget", native_budget)
                    telemetry.refresh(live=True)
                    telemetry_updated = True
                    background = telemetry.background()
                    tasks = (background or {}).get("tasks", [])
                    active = any(t.get("status") in {"queued", "running", "waiting_dependencies", "waiting_approval", "waiting_question"} for t in tasks)
                    terminal_deliveries = {"delivered", "failed", "stopped"}
                    waiting = any(not t.get("notified") and t.get("delivery") not in terminal_deliveries for t in tasks)
                    failed_deliveries = [t for t in tasks if not t.get("notified") and t.get("delivery") == "failed"]
                    # Standard-mode runs settle by their public result; Super also
                    # needs background deliveries and the coordinator to settle.
                    state_known = mode != "super-agent" or background is not None
                    if state_known and not view.get("run_id") and not active and not waiting and failed_deliveries:
                        failed = failed_deliveries[0]
                        stop = "delivery_failed"
                        failure = "background result notification failed for task " + str(failed.get("id", "unknown")) + ": " + str(failed.get("delivery_error", "unknown delivery error"))
                        break
                    settled = state_known and task_result_seen and not view.get("run_id") and not active and not waiting
                    if settled:
                        quiet_since = quiet_since or time.monotonic()
                        if time.monotonic() - quiet_since >= 3:
                            stop = "settled"
                            break
                        poll_interval = CONTEXT_SNAPSHOT_INTERVAL
                    else:
                        quiet_since = None
                        poll_interval = min(MAX_CONTEXT_SNAPSHOT_INTERVAL, poll_interval * 2)
            if frame.get("type") == "control_request":
                request = frame.get("request", {})
                if request.get("subtype") == "can_use_tool":
                    # Always deny host-mode escalation in scored trials. The
                    # allow policy only answers normal disposable-workspace requests.
                    args = request.get("input", {})
                    host = isinstance(args, dict) and args.get("sandbox_mode") == "host"
                    allow = policy == "allow" and not host
                    interactions["host_requests"] += int(host)
                    interactions["allowed" if allow else "denied"] += 1
                    answer = {"behavior": "allow" if allow else "deny"}
                else:
                    interactions["questions_rejected"] += 1
                    answer = {"rejected": True}
                send({"type": "control_response", "response": {"subtype": "success", "request_id": frame["request_id"], "response": answer}})
            if frame.get("type") == "result" and (frame.get("user_message_uuid") == "task" or
                                                  frame.get("user_message_uuid") in coordinator_turns):
                task_result_seen = True
                # Keep final delivery settlement responsive even after a long
                # background wait has increased the status-poll interval.
                poll_interval = CONTEXT_SNAPSHOT_INTERVAL
                final_text = frame.get("result", "")
                if frame.get("user_message_uuid") == "task":
                    final_visible = bool(final_text.strip())
                if frame.get("is_error"):
                    if frame.get("stop_reason") == "call_budget" and frame.get("subtype") == "error_call_budget":
                        stop = "call_budget"
                        native_budget = frame.get("execution_budget", native_budget)
                    elif frame.get("stop_reason") in ("step_limit", "incomplete") and frame.get("subtype") == "error_" + frame["stop_reason"]:
                        stop = frame["stop_reason"]
                    else:
                        stop, failure = "harness_error", frame.get("subtype", "task failed")
                    break
            if task_sent and budgets["model_calls"] is not None and time.monotonic() - last_budget_check >= BUDGET_CHECK_INTERVAL:
                if not telemetry_updated:
                    telemetry.refresh(live=True)
                cost = telemetry.model_cost()
                last_budget_check = time.monotonic()
                if cost["model_calls"] is not None and cost["model_calls"] >= receipt["budgets"]["model_calls"]:
                    # Native admission owns the exact ceiling. Let admitted
                    # work settle under the configured wall-clock budget.
                    if not native_budget or cost["model_calls"] > receipt["budgets"]["model_calls"]:
                        stop = "call_budget"
                        break
                if cost["observed_total_tokens"] is not None and cost["observed_total_tokens"] >= receipt["budgets"]["total_tokens"]:
                    stop = "token_budget"
                    break

            if task_sent and not pending_poll and time.monotonic() - last_poll >= poll_interval:
                control("poll", "context.snapshot")
                last_poll, pending_poll = time.monotonic(), True
    except Exception as exc:
        failure = f"{type(exc).__name__}: {exc}"
    finally:
        if proc is not None:
            if proc.poll() is None:
                try:
                    control("shutdown", "shutdown", mode="cancel")
                    proc.wait(timeout=5)
                except (OSError, subprocess.TimeoutExpired):
                    stop_process(proc)
            stop_process(proc)
            if proc.stdin:
                proc.stdin.close()
            if 'thread' in locals():
                thread.join(timeout=3)
            if proc.stdout:
                proc.stdout.close()
        trace.close()
        errors.close()
    execution_elapsed = time.monotonic() - started
    return {'final_text': final_text, 'final_visible': final_visible, 'task_result_seen': task_result_seen, 'stop': stop, 'failure': failure, 'execution_elapsed': execution_elapsed, 'model_selection': model_selection, 'server_info': server_info, 'config_observed': config_observed, 'native_budget': native_budget, 'interactions': interactions}


def _run_trial(case, binary, model, mode, telemetry, directory, repeat, policy, config_fingerprint, workspace, parallel_step_sharing=False, no_execution_budget=False):
    directory = Path(directory)
    directory.mkdir(parents=True, exist_ok=False)
    layout = TrialLayout(directory)
    layout.prepare()
    repository = case['manifest'].get('repository')
    copy_tree(case['directory'], directory / 'case')
    case = {**case, 'paths': {k: directory / 'case' / v.relative_to(case['directory']) if v is not None else None
                            for k, v in case['paths'].items()}, 'directory': directory / 'case'}
    prompt = case["paths"]["prompt"].read_text()
    receipt = {"schema_version": 1, "layout_version": 2, "trial_id": directory.name, "case_id": case["manifest"]["id"],
               "case_version": case["manifest"]["version"], "case_fingerprint": case["fingerprint"],
               "case_receipt": case["receipt"], "workspace": str(workspace.resolve()), "repeat": repeat, "model_requested": model, "mode": mode,
               "interaction_policy": policy, "external_config_fingerprint": config_fingerprint,
               "binary_sha256": digest(Path(binary).read_bytes()), "environment": environment_receipt(),
               "budgets": case["manifest"]["budgets"], "telemetry_adapter": "nekocode-local-journal-v1", "evaluator_fingerprint": evaluator_fingerprint(),
               "evaluation_contract_version": EVALUATION_CONTRACT_VERSION}
    if no_execution_budget:
        if parallel_step_sharing:
            raise ValueError("parallel step sharing requires an execution budget")
        receipt["budgets"] = {**receipt["budgets"], "model_calls": None, "total_tokens": None, "wall_seconds": None}
    receipt["parallel_step_sharing"] = parallel_step_sharing
    write_json(directory / "receipt.json", receipt)
    write_json(directory / 'state.json', {'status': 'preparing'})
    preparation_started = time.monotonic()
    try:
        verifier_sandbox.preflight(case)
        if repository:
            receipt['repository'] = prepare_repository(case, workspace, layout.trace('preparation.log'))
        else:
            copy_tree(case['paths']['fixture'], workspace)
        with layout.trace('preparation.log').open('a') as log:
            for args in [['init', '-q'], ['add', '-f', '.'], ['-c', 'user.name=Eval', '-c', 'user.email=eval@localhost', 'commit', '-qm', 'eval seed']]:
                run_git('-C', str(workspace), *args, log=log)
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as exc:
        return _preparation_failed(case, directory, receipt, telemetry, preparation_started, exc)
    receipt['preparation_elapsed_s'] = round(time.monotonic() - preparation_started, 3)
    write_json(directory / 'receipt.json', receipt)
    write_json(directory / "state.json", {"status": "running"})
    execution = execute_agent(binary, workspace, model, mode, policy, prompt, receipt, telemetry, layout)
    verification_started = time.monotonic()
    telemetry.refresh()
    relevant_runs = {e.get("run_id") for e in telemetry.event_rows("run_started")
                     if (e.get("source", {}).get("kind") == "task_completion" or e.get("source", {}).get("id") == "task")}
    failed_runs = [e.get("run_id") for e in telemetry.event_rows("run_failed", "run_aborted")
                   if e.get("run_id") in relevant_runs]
    if execution['stop'] == "settled" and failed_runs:
        execution['stop'], execution['failure'] = "harness_error", "task/coordinator runtime failure: " + ", ".join(failed_runs)
    observed_model_fingerprint, auxiliary_models = model_observation(execution['model_selection'], list(telemetry.calls.values()))
    if case["manifest"]["id"] == "relay-station":
        receipt["workspace_change_ns"] = workspace_change_ns(workspace)
    receipt["auxiliary_models"] = auxiliary_models
    receipt["observed_model_fingerprint"] = observed_model_fingerprint
    write_json(directory / "receipt.json", receipt)
    (directory / "answer.txt").write_text(execution['final_text'])
    write_json(layout.trace("observed.json"), {"model": execution['model_selection'], "server": execution['server_info'], "extensions": execution['config_observed'], "session": telemetry.session})
    for filename, rows in [("calls.jsonl", telemetry.safe_calls()), ("events.jsonl", telemetry.event_rows())]:
        with layout.trace(filename).open("w") as stream:
            for row in rows:
                stream.write(json.dumps(row, ensure_ascii=False) + "\n")
    if repository:
        # The verifier checks the temporary review tree against the trusted
        # pre-execution digest. No source copies enter the trial archive.
        product_hash = None
        verification = verify(case, workspace, layout.verification, execution['final_text'],
                              input_sha256=receipt['repository']['input_sha256'])
    else:
        # Products stay frozen; generated code is verified in a disposable copy.
        copy_tree(workspace, directory / 'product')
        product_hash = tree_hash(directory / 'product')
        verification = verify(case, directory / 'product', layout.verification, execution['final_text'],
                              events_path=layout.trace('events.jsonl'), original_workspace=workspace,
                              workspace_change_ns=receipt.get("workspace_change_ns"))
    background_snapshot = telemetry.background()
    write_json(layout.trace("tasks.json"), background_snapshot if background_snapshot is not None else {"available": False})
    coordination = coordination_audit(telemetry.event_rows(), background_snapshot)
    cost = telemetry.cost()
    cost.update(protocol_tool_cost(layout.trace("protocol.jsonl"), telemetry.session))
    completeness = {"calls_observed": bool(telemetry.calls), "events_observed": cost["tool_started"] is not None,
                    "public_task_result": execution['task_result_seen'], "background_state_observed": telemetry.background() is not None,
                    "all_call_ids_present": bool(telemetry.calls) and all(c.get("call_id") for c in telemetry.calls.values()),
                    "tool_counter_scope": {"repeated_tool_requests": "same name and JSON arguments with a distinct call ID in the same root run; includes legitimate rechecks", "tool_requests": "root task protocol, unique run/call IDs", "tool_result_errors": "root task protocol is_error results", "tool_started": "local session runtime events", "tool_blocked": "local session runtime events", "tool_calls": "compatibility alias of tool_started"},
                    "limitations": ["image_understand attempts are logged; providers can omit usage. Unknown usage is not zero, and token caps use the observed subtotal as a lower bound. Other uninstrumented model tools may still be absent.", "Local journal/calllog adapter; rotating or disabled logs can undercount.",
                                    ("Usage observed without execution cutoffs." if no_execution_budget else "Native model admission counts in-flight requests; admitted work settles under the wall-clock budget. Token cap still polls observed usage and can overshoot." if execution['native_budget'] else "Call/token caps are checked after completed physical calls; in-flight calls can overshoot."),
                                    "External configuration is not isolated; an explicit config fingerprint is required for strict comparison.",
                                    "Verifier and product checks run under Linux bubblewrap in private namespaces. This does not isolate the model execution stage or adjudicate malicious grading manipulation."]}
    if telemetry.read_errors:
        completeness["telemetry_read_errors"] = dict(telemetry.read_errors)
        completeness["limitations"].append("Unreadable telemetry files can leave observed counts incomplete.")
    outcome = {"schema_version": 1, "layout_version": 2, "trial_id": directory.name, "session": telemetry.session, "stop_reason": execution['stop'],
               "error": execution['failure'], "elapsed_s": round(execution['execution_elapsed'], 3), "verification_elapsed_s": round(time.monotonic() - verification_started, 3), "cost": cost,
               "verification": verification, "success": execution['stop'] == "settled" and verification["product_passed"] and not execution['failure'],
               "coordination": coordination, "execution_budget": execution['native_budget'], "interactions": execution['interactions'], "integrity": completeness, "product_sha256": product_hash, "product_retained": not bool(repository),
               "public_initial_answer_present": execution['final_visible'], "public_final_answer_present": bool(execution['final_text'].strip()),
               "failed_runtime_runs": failed_runs}
    write_json(directory / "outcome.json", outcome)
    write_json(directory / "state.json", {"status": "finished"})
    report = [f"# {case['manifest']['title']} / {directory.name}", "", f"- End: {execution['stop']}", f"- Successful delivery: {outcome['success']}",
              f"- Required checks: {verification['required_passed']}/{verification['required_total']}",
              f"- Model calls: {cost['model_calls']}; total tokens: {cost['total_tokens']}; fresh: {cost['fresh_tokens']}",
              f"- Tool model calls: {cost['tool_model_calls']}; calls with unknown usage: {cost['usage_unknown_calls']}; observed token subtotal (lower bound): {cost['observed_total_tokens']}",
              f"- Tool requests (root wire): {cost['tool_requests']}; started (session runtime): {cost['tool_started']}; blocked: {cost['tool_blocked']}; error results (root wire): {cost['tool_result_errors']}",
              f"- Root tool-call rounds: {cost['tool_rounds']}",
              f"- Root tool-call rounds before first successful read: {cost['tool_rounds_before_first_read']}",
              f"- Repeated root requests (same tool and arguments, includes legitimate rechecks): {cost['repeated_tool_requests']}",
              f"- Same-image rechecks (identical ordered image bytes): {cost['same_image_rechecks']}; image HTTP 429 results: {cost['image_rate_limits']}",
              f"- Context snapshot polls (harness control traffic): {cost['context_snapshot_polls']}",
              f"- Execution seconds (including shutdown, excluding verification): {outcome['elapsed_s']}",
              f"- Host-mode requests denied: {execution['interactions']['host_requests']}"]
    if execution["stop"] == "call_budget" and execution["native_budget"]:
        b = execution["native_budget"]
        report.append(f"- Call budget: {b['admitted']}/{b['limit']} admitted; remaining ordinary {b['work_remaining']}, worker handoff {b['handoff_remaining']}, coordinator {b['coordinator_remaining']}")
    report += ["", "## Independent checks", ""]
    report += [f"- {c['id']}: {c['status']} — {c['evidence']}" for c in verification["checks"]]
    report += ['', '## Files', '']
    if repository:
        report += ['- [Pinned repository and input digest](receipt.json)',
                   '- Source workspace was temporary and is removed after verification. No full product copy is retained.']
    else:
        report += ['- [Final product](product/)']
    report += ['- [Agent answer](answer.txt)', '- [Trace](trace/)',
               '- [Verification](verification/v1/results/verification.json)']
    report += ["", "## Interpretation", "", "Product acceptance, runtime settlement and observation completeness are separate. Inspect trace/ for execution evidence."]
    (directory / "report.md").write_text("\n".join(report) + "\n")
    return outcome
