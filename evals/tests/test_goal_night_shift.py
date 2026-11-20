import importlib.util
import unittest

from agent_eval.catalog import load_case


case = load_case("goal-night-shift")
spec = importlib.util.spec_from_file_location("night_shift_verifier", case["paths"]["verifier"])
verifier = importlib.util.module_from_spec(spec)
spec.loader.exec_module(verifier)


class NightShiftProgressionTests(unittest.TestCase):
    def test_two_separate_checkpoints_require_work_in_each_phase(self):
        def event(kind, **fields):
            return {"run_id": "main", "type": kind, **fields}

        start = event("run_started", source={"kind": "headless", "id": "task"})
        work = event("tool_completed", payload={"tool_name": "write", "goal_progress": True,
                                                "evidence": {"executions": [{"status": "success"}]}})
        next_turn = [event("assistant_delta", payload={"delta": "Progress\nGOAL N"}),
                     event("assistant_delta", payload={"delta": "EXT"}), event("assistant_reset")]
        reset_only = next_turn
        next_turn = [event("system_message", source={"kind": "goal"}, payload={"goal_continuation": 1})]
        review = event("system_message", source={"kind": "goal"},
                       payload={"content": "Goal awaits your review."})
        def passes(events):
            return verifier.progression(events)[0]

        self.assertFalse(passes([start, work, *reset_only, work, *reset_only, work, review]))
        phases = [start, work, *next_turn, work, *next_turn]
        self.assertTrue(passes([*phases, work, review]))
        idle = event("tool_completed", payload={"tool_name": "process"})
        self.assertFalse(passes([start, idle, *next_turn, idle, *next_turn, idle, review]))
        self.assertFalse(passes([start, work, *next_turn, *next_turn, work, review]))
        self.assertFalse(passes([*phases, review]))
        self.assertFalse(passes([*phases, dict(work, run_id="other"), review]))
        self.assertFalse(passes([*phases, work, dict(review, source={"kind": "bot"})]))
        failed = event("tool_completed", payload={"goal_progress": True, "evidence": {
            "executions": [{"status": "success"}, {"status": "error", "exit_code": 7}]}})
        self.assertFalse(passes([start, failed, *next_turn, failed, *next_turn, failed, review]))
        other = [dict(e, run_id="second") for e in [start, work, *next_turn, work, review]]
        self.assertFalse(passes([start, work, *next_turn, *other]))
        self.assertFalse(passes([*phases, review, work]))
        self.assertFalse(passes([start, *next_turn, *phases[1:], work, review]))
