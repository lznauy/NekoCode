import json
import os
from pathlib import Path
from runpy import run_path
import tempfile
import unittest

from agent_eval.storage import workspace_change_ns

module = run_path(str(Path(__file__).resolve().parents[1] / "cases/relay-station/collaboration.py"))
checks = module["collaboration_checks"]


class Journal:
    def __init__(self):
        self.rows = []
        self.add("run_started", source={"kind": "headless", "id": "task"})

    def add(self, kind, *, run="root", source=None, **payload):
        self.rows.append({"type": kind, "sequence": len(self.rows) + 1, "run_id": run,
                          "source": source or {"kind": "bot"}, "payload": payload})

    def start(self, child, attempt="one", status="running"):
        self.add("subagent_started", id=child, attempt_id=attempt, status=status)

    def end(self, child, attempt="one", status="completed"):
        self.add("subagent_ended", id=child, attempt_id=attempt, status=status)

    def tool(self, name="read", child="", attempt="", state=None, args=None, record_attempt=None,
             run="root", is_error=False):
        call = f"call{len(self.rows)}"
        base = dict(tool_name=name, call_id=call, subagent_id=child, attempt_id=attempt, input=args or {})
        self.add("tool_started", run=run, **base)
        execution = dict(run_id=run, task_id=child, attempt_id=attempt if record_attempt is None else record_attempt)
        status = "unknown" if (state or {}).get("state") == "running" else "success"
        record = dict(id=call, call_id=call, tool=name, status=status, execution=execution, process=state or {}, ended_at=len(self.rows))
        self.add("tool_completed", run=run, **base, evidence={"executions": [record]}, is_error=is_error)

    def wave(self, count):
        ids = [f"worker-{i}" for i in range(count)]
        for child in ids:
            self.start(child, attempt=None)
        for child in ids:
            self.tool(child=child, attempt="one")
            self.end(child)

    def suite(self, state=None):
        self.tool("shell", args={"command": "python3 -m unittest discover -s tests -v"},
                  state=state or {"state": "exited", "exit_code_known": True})

    def results(self, workspace_change_ns=None):
        with tempfile.TemporaryDirectory() as root:
            trace = Path(root) / "events.jsonl"
            # The exporter may interleave polls from different journal files.
            trace.write_text("\n".join(map(json.dumps, reversed(self.rows))))
            return {c["id"]: c["status"] for c in checks(trace, workspace_change_ns)}


class RelayCollaborationTests(unittest.TestCase):
    def test_repeated_queue_end_projects_one_transition(self):
        journal = Journal()
        for waiting in (True, False, False, False):
            journal.add("interaction_queue", subagent_id="queued", id=1, waiting=waiting, source={"kind": "runtime"})
        events = module["projected_events"](reversed(journal.rows))
        self.assertEqual([(e[0], e[2]) for e in events], [(2, "requested"), (3, "resolved")])

    def test_integration_command_shell_continuations(self):
        continuation = "\\\n"
        for middle, expected in [
            ("unittest " + continuation + "  discover", "passed"),
            ('"unit' + continuation + 'test" discover', "passed"),
            ("'unit" + continuation + "test' discover", "failed"),
            ("unittest \\" + continuation + "discover", "failed"),
            ("unittest\ndiscover", "failed"),
        ]:
            with self.subTest(middle=middle):
                journal = Journal()
                journal.wave(12)
                journal.tool("shell", args={"command": "python3 -m " + middle + " -s tests"},
                             state={"state": "exited", "exit_code_known": True})
                self.assertEqual(journal.results()["integration_run"], expected)

    def test_pending_runtime_requests_exclude_worker_until_all_resolve(self):
        for resolve_all in (False, True):
            with self.subTest(resolve_all=resolve_all):
                journal = Journal()
                journal.start("waiting")
                source = {"kind": "runtime"}
                for family in ("approval", "question"):
                    journal.add(family + "_requested", subagent_id="waiting", id=family, source=source)
                journal.add("subagent_updated", id="waiting", attempt_id="one", status="running")
                journal.add("approval_resolved", subagent_id="waiting", id="approval", source=source)
                if resolve_all:
                    journal.add("question_resolved", subagent_id="waiting", id="question", source=source)
                journal.wave(7)
                journal.tool(child="waiting", attempt="one")
                journal.end("waiting")
                self.assertEqual(journal.results()["parallelism"], "passed" if resolve_all else "failed")

    def test_queued_child_does_not_count_before_question_is_registered(self):
        journal = Journal()
        journal.start("queued")
        journal.add("interaction_queue", subagent_id="queued", id=1, waiting=True, source={"kind": "runtime"})
        journal.wave(7)
        journal.add("interaction_queue", subagent_id="queued", id=1, waiting=False, source={"kind": "runtime"})
        journal.tool(child="queued", attempt="one")
        journal.end("queued")
        self.assertEqual(journal.results()["parallelism"], "failed")

    def test_invalid_shell_command_keeps_other_verdicts(self):
        journal = Journal()
        journal.wave(12)
        journal.tool("shell", args={"command": 42}, is_error=True)
        self.assertEqual(journal.results()["integration_run"], "failed")
        journal.suite()
        journal.tool("shell", args={"command": 42}, is_error=True)
        self.assertEqual(set(journal.results().values()), {"passed"})

    def test_success_with_background_final_suite(self):
        journal = Journal()
        journal.wave(12)
        journal.suite({"state": "running", "task": "job"})
        journal.tool("process", state={"state": "exited", "task": "job", "exit_code_known": True},
                     args={"action": " WAIT ", "task": " job "})
        self.assertEqual(set(journal.results().values()), {"passed"})

    def test_long_child_process_counts_only_for_its_launch_attempt(self):
        for handle, expected in [("job", "passed"), ("other", "failed")]:
            with self.subTest(handle=handle):
                journal = Journal()
                journal.wave(11)
                journal.start("tester")
                journal.tool("shell", "tester", "one", state={"state": "running", "task": "job"})
                journal.tool("process", "tester", "one", args={"action": "wait", "task": handle},
                             state={"task": handle, "state": "exited", "exit_code_known": True})
                journal.end("tester")
                self.assertEqual(journal.results()["workers"], expected)

    def test_post_test_inspection_retains_integration_evidence(self):
        journal = Journal()
        journal.wave(12)
        journal.suite()
        journal.tool("shell", args={"command": "git status --short"})
        journal.tool("shell", args={"command": "git diff --check"})
        self.assertEqual(journal.results()["integration_run"], "passed")
        journal.tool("shell", args={"command": "git status --short > report"})
        self.assertEqual(journal.results()["integration_run"], "failed")

    def test_workspace_observation_distinguishes_shell_reads_and_later_changes(self):
        journal = Journal()
        journal.wave(12)
        before = len(journal.rows)
        journal.suite()
        journal.tool("shell", args={"command": "ls -l README.md"})
        self.assertEqual(journal.results(before)["integration_run"], "passed")
        self.assertEqual(journal.results(len(journal.rows))["integration_run"], "failed")

    def test_workspace_observation_uses_inode_times_and_includes_directories(self):
        with tempfile.TemporaryDirectory() as root:
            project = Path(root) / "project"
            project.mkdir()
            module = project / "relay.py"
            module.write_text("first")
            before_read = workspace_change_ns(project)
            module.read_bytes()
            self.assertEqual(workspace_change_ns(project), before_read)
            module.write_text("changed")
            os.utime(module, ns=(1, 1))
            self.assertEqual(workspace_change_ns(project),
                             max(module.stat().st_ctime_ns, project.stat().st_ctime_ns))
            module.unlink()
            self.assertEqual(workspace_change_ns(project), project.stat().st_ctime_ns)

    def test_post_test_edit_must_have_matching_non_execution_evidence(self):
        journal = Journal()
        journal.wave(12)
        journal.suite()
        call = {"tool_name": "edit", "call_id": "late-edit", "input": {"path": "relay/model.py"}}
        journal.add("tool_started", **call)
        self.assertEqual(journal.results()["integration_run"], "failed")
        record = {"id": "blocked", "call_id": "late-edit", "tool": "edit", "execution": {"run_id": "root"}}
        journal.add("tool_completed", **call, is_error=True, non_execution=record)
        self.assertEqual(journal.results()["integration_run"], "passed")
        record["call_id"] = "another-call"
        self.assertEqual(journal.results()["integration_run"], "failed")

    def test_queued_and_serial_workers_do_not_inflate_peak(self):
        journal = Journal()
        for child in map(str, range(12)):
            journal.start(child, status="queued")
        for child in map(str, range(12)):
            journal.start(child)
            journal.tool(child=child, attempt="one")
            journal.end(child)
        journal.suite()
        self.assertEqual(journal.results(), {"workers": "passed", "parallelism": "failed", "integration_run": "passed"})

    def test_failed_attempts_failed_tools_and_bookkeeping_do_not_count(self):
        journal = Journal()
        journal.wave(7)
        for i, name in enumerate(("todo_write", "submit_result", "shell", "read", "read", "read", "read")):
            child = str(i)
            journal.start(child)
            journal.tool(name, child, "one", state={"state": "exited", "exit_code_known": True, "exit_code": 3},
                         is_error=i == 3, record_attempt="stale" if i == 4 else None, run="other" if i == 6 else "root")
            journal.end(child, status="partial" if i == 5 else "completed")
        for i in range(10):
            journal.start("worker-0", attempt=str(i))
            journal.tool(child="worker-0", attempt=str(i))
            journal.end("worker-0", attempt=str(i))
        result = journal.results()
        self.assertEqual((result["workers"], result["parallelism"]), ("failed", "failed"))

    def test_early_wrong_and_stale_integrations_fail(self):
        journal = Journal()
        journal.suite()
        journal.wave(12)
        journal.suite({"state": "running", "task": "expected"})
        journal.tool("process", args={"action": "wait", "task": "other"},
                     state={"state": "exited", "task": "other", "exit_code_known": True})
        self.assertEqual(journal.results()["integration_run"], "failed")
        journal.suite()
        journal.suite({"state": "exited", "exit_code_known": True, "exit_code": 2})
        self.assertEqual(journal.results()["integration_run"], "failed")
        journal.suite()
        journal.tool("edit")
        self.assertEqual(journal.results()["integration_run"], "failed")

    def test_child_approval_waits_do_not_count_toward_overlap(self):
        journal = Journal()
        for child in map(str, range(12)):
            journal.start(child)
            journal.add("subagent_updated", id=child, status="waiting_approval", attempt_id="one")
        for child in map(str, range(12)):
            journal.add("subagent_updated", id=child, status="running", attempt_id="one")
            journal.tool(child=child, attempt="one")
            journal.end(child)
        self.assertEqual(journal.results()["parallelism"], "failed")

    def test_missing_journal(self):
        self.assertEqual({c["status"] for c in checks(None)}, {"not_run"})


if __name__ == "__main__":
    unittest.main()
