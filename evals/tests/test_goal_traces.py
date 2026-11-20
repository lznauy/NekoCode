from pathlib import Path
from runpy import run_path
import unittest


class GoalTraceTests(unittest.TestCase):
    def test_verifiers_agree_on_phase_evidence(self):
        cases = Path(__file__).resolve().parents[1] / "cases"
        checkpoint = run_path(str(cases / "goal-checkpoint" / "verify.py"))["lifecycle"]
        night_shift = run_path(str(cases / "goal-night-shift" / "verify.py"))["progression"]

        def event(kind, source=None, **payload):
            return {"type": kind, "run_id": "first", "source": source or {}, "payload": payload}

        start = event("run_started", {"kind": "headless", "id": "task"})
        work = event("tool_completed", goal_progress=True, evidence={"executions": [{"status": "success"}]})
        failed = event("tool_completed", goal_progress=True, evidence={"executions": [{"status": "error"}]})
        advance = event("system_message", {"kind": "goal"}, goal_continuation=1)
        review = event("system_message", {"kind": "goal"}, content="Goal awaits your review.")
        valid = [start, work, advance, work, advance, work, review]
        for name, rows, short_ok, game_ok in [
            ("single run", valid, True, True),
            ("split runs", valid[:3] + [dict(e, run_id="second") for e in [start, *valid[3:]]], False, False),
            ("review before continuation", [start, work, review, advance, work], False, False),
            ("empty final phase", valid[:-2] + [review], True, False),
            ("failed commands", [failed if e is work else e for e in valid], False, False),
        ]:
            with self.subTest(name=name):
                # The short case only requires work across one continuation.
                self.assertEqual(all(checkpoint(rows)), short_ok)
                self.assertEqual(night_shift(rows)[0], game_ok)
