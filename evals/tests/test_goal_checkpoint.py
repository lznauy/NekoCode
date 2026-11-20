import json
from pathlib import Path
import shutil
import tempfile
import unittest

from agent_eval.catalog import load_case
from agent_eval.verification import verify


class GoalCheckpointTests(unittest.TestCase):
    def test_artifact_alone_cannot_pass_and_claims_cannot_replace_artifact(self):
        case = load_case("goal-checkpoint")
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            workspace = root / "workspace"
            shutil.copytree(case["paths"]["fixture"], workspace)
            report = workspace / "net.csv"
            report.write_text("route,net_seats\nnorth,14\nsouth,9\n")
            perfect = verify(case, workspace, root / "without-trace", "GOAL NEXT\nGOAL REVIEW")
            self.assertEqual({c["id"]: c["status"] for c in perfect["checks"]},
                             {"continuation": "not_run", "delivery": "passed", "review": "not_run"})

            trace = root / "events.jsonl"
            trace.write_text("\n".join(json.dumps(dict(run_id="run1", **e)) for e in [
                {"type": "run_started", "source": {"kind": "headless", "id": "task"}},
                {"type": "assistant_delta", "payload": {"delta": "GOAL NEXT"}},
                {"type": "assistant_message", "payload": {"content": "Goal awaits your review."}},
            ]))
            report.write_text("route,net_seats\nnorth,17\nsouth,11\n")
            claims = verify(case, workspace, root / "claims", "GOAL REVIEW", events_path=trace)
            self.assertIsNone(claims["error"], claims)
            self.assertTrue(all(c["status"] == "failed" for c in claims["checks"]), claims)

    def test_bookkeeping_cannot_qualify_a_checkpoint(self):
        from runpy import run_path
        lifecycle = run_path(str(load_case("goal-checkpoint")["paths"]["verifier"]))["lifecycle"]
        def event(kind, source=None, **payload):
            return {"run_id": "run", "type": kind, "source": source or {}, "payload": payload}
        start = event("run_started", {"kind": "headless", "id": "task"})
        follow = event("system_message", {"kind": "goal"}, goal_continuation=1)
        review = event("system_message", {"kind": "goal"}, content="Goal awaits your review.")
        work = event("tool_completed", tool_name="write", goal_progress=True, evidence={"executions": [{"status": "success"}]})
        todo = event("tool_completed", tool_name="todo_write")
        self.assertEqual(lifecycle([start, todo, follow, todo, review]), (False, True))
        self.assertEqual(lifecycle([start, work, follow, work, review]), (True, True))
        failed = event("tool_completed", tool_name="shell", goal_progress=True,
                       evidence={"executions": [{"status": "error", "exit_code": 7}]})
        self.assertEqual(lifecycle([start, work, follow, failed, review]), (False, True))
