import json
from pathlib import Path
import shutil
import tempfile
import unittest

from agent_eval.catalog import load_case
from agent_eval.verification import verify


class ShortTasks(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.root = Path(tmp.name)
        self.workspace = self.root / "workspace"

    def load(self, case_id):
        self.case = load_case(case_id)
        shutil.copytree(self.case["paths"]["fixture"], self.workspace)

    def failed(self, label):
        result = verify(self.case, self.workspace, self.root / label)
        self.assertIsNone(result["error"])
        return {check["id"] for check in result["checks"] if check["status"] != "passed"}

    def test_config_isolation(self):
        self.load("retry-settings")
        path = self.workspace / "config/client.json"
        self.assertEqual(self.failed("seed"), {"active"})
        data = json.loads(path.read_text())
        data["profiles"]["production"]["retry"] = dict(attempts=4, delays_ms=[100, 300, 900])
        path.write_text(json.dumps(data))
        self.assertFalse(self.failed("fixed"))
        data["profiles"]["preview"]["connect_ms"] = 2500
        path.write_text(json.dumps(data))
        self.assertEqual(self.failed("collateral"), {"preserved"})

    def test_schedule_detects_repeated_opponents_and_fixed_court(self):
        self.load("round-robin-card")
        teams = (self.workspace / "teams.txt").read_text().splitlines()
        rows = []
        for i in range(5):
            matches = [f"{teams[j]} vs {teams[-1-j]}" for j in range(3)]
            rows.append(matches)
            teams = teams[:1] + teams[-1:] + teams[1:-1]
        def save():
            body = ["|轮次|一号场|二号场|三号场|", "|---|---|---|---|"]
            body.extend("|" + "|".join([str(i), *matches]) + "|" for i, matches in enumerate(rows, 1))
            (self.workspace / "schedule.md").write_text("\n".join(body))
        save()
        self.assertEqual(self.failed("fixed-court"), {"rotation"})
        for i, row in enumerate(rows):
            n = i % 3
            rows[i] = row[n:] + row[:n]
        save()
        self.assertFalse(self.failed("balanced"))
        rows[-1] = rows[0]
        save()
        self.assertEqual(self.failed("duplicate"), {"pairs"})

    def test_traceback_diagnosis_requires_cause_and_unchanged_files(self):
        self.load("unittest-triage")
        answer = {"test": "test_charge_completed_units", "actual_key": "reserved", "expected_key": "completed"}

        def statuses(label, value):
            result = verify(self.case, self.workspace, self.root / label, json.dumps(value))
            self.assertIsNone(result["error"])
            return {check["id"] for check in result["checks"] if check["status"] != "passed"}

        self.assertFalse(statuses("correct", answer))
        self.assertEqual(statuses("wrong", dict(answer, actual_key="completed")), {"diagnosis"})
        (self.workspace / "totals.py").write_text("")
        self.assertEqual(statuses("changed", answer), {"unchanged"})
