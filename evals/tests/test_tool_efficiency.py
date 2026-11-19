import json
from pathlib import Path
import shutil
import tempfile
import unittest

from agent_eval.catalog import load_case
from agent_eval.verification import verify


class EfficiencyCases(unittest.TestCase):
    def prepare(self, name):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.case = load_case(name)
        self.workspace = self.root / "workspace"
        shutil.copytree(self.case["paths"]["fixture"], self.workspace)

    def failed(self, label, answer=None):
        result = verify(self.case, self.workspace, self.root / label, json.dumps(answer))
        self.assertIsNone(result["error"])
        return {r["id"] for r in result["checks"] if r["status"] != "passed"}

    def test_dispatch_requires_all_positive_intersections_and_intact_sources(self):
        self.prepare("dispatch-window")
        output = self.workspace / "windows.csv"
        valid = "site,start,end\neast,08:30,09:00\nwest,10:00,11:00\nwest,13:00,14:00\n"
        output.write_text(valid)
        self.assertEqual(self.failed("ok"), set())
        output.write_text(valid + "north,06:00,06:00\n")
        self.assertEqual(self.failed("endpoint"), {"windows"})
        output.write_text(valid)
        (self.workspace / "coverage.csv").write_text("")
        self.assertEqual(self.failed("collateral"), {"scope"})

    def test_effective_limit_rejects_redundant_override_and_false_success(self):
        self.prepare("effective-limit")
        answer = {"effective": 6, "changed": False}
        self.assertEqual(self.failed("ok", answer), set())
        self.assertEqual(self.failed("wrong-type", dict(answer, changed=0)), {"decision"})
        path = self.workspace / "tenant.json"
        tenants = json.loads(path.read_text())
        tenants["north"]["parallelism"] = 6
        path.write_text(json.dumps(tenants))
        self.assertEqual(self.failed("redundant", answer), {"scope"})

    def test_routing_rejects_stale_release_and_new_files(self):
        self.prepare("active-routing")
        answer = {"release": "r31", "queue": "jade", "source": "tables/east-04.csv"}
        self.assertEqual(self.failed("ok", answer), set())
        self.assertEqual(self.failed("previous", dict(answer, queue="blue")), {"routing"})
        (self.workspace / "notes.txt").touch()
        self.assertEqual(self.failed("collateral", answer), {"scope"})

    def test_inline_handoff_uses_latest_message_and_preserves_archive(self):
        self.prepare("inline-handoff")
        current = {"incident": "C-71", "owner": "Rao", "next_check": "09:45"}
        self.assertFalse(self.failed("current", current))
        for label, body in [("fenced", "```json\n" + json.dumps(current) + "\n```"),
                            ("with-prose", json.dumps(current) + "\n已核对")]:
            result = verify(self.case, self.workspace, self.root / label, body)
            self.assertIsNone(result["error"])
            self.assertFalse(result["product_passed"])
        self.assertEqual(self.failed("obsolete", dict(current, owner="Lin")), {"handoff"})
        (self.workspace / "handoff.txt").unlink()
        self.assertEqual(self.failed("archive-missing", current), {"scope"})

    def test_approvals_require_matching_revision_status_and_ticket(self):
        self.prepare("approval-reconcile")
        current = {"M14": {"status": "unconfirmed", "receipt": None},
                   "M19": {"status": "approved", "receipt": "S37"}}
        self.assertFalse(self.failed("current", current))
        for receipt in ("S92", "S95", "S37"):
            answer = dict(current, M14={"status": "approved", "receipt": receipt})
            self.assertEqual(self.failed(receipt, answer), {"gap"})
        self.assertEqual(self.failed("always-abstain", dict(current, M19=current["M14"])), {"positive"})
        (self.workspace / "memo.txt").touch()
        self.assertEqual(self.failed("extra-file", current), {"scope"})

    def test_roster_tsv_must_be_directly_importable(self):
        self.prepare("current-roster-tsv")
        valid = "team\towner\nnavy\t杜\nplum\t周"
        for label, text, expected in [
            ("valid", valid + "\n", True),
            ("fenced", "```tsv\n" + valid + "\n```", False),
            ("inactive", valid + "\ngreen\t宋", False),
            ("spaces", valid.replace("\t", " "), False),
        ]:
            result = verify(self.case, self.workspace, self.root / label, text)
            self.assertIsNone(result["error"])
            self.assertEqual(result["product_passed"], expected)

    def test_wiki_example_requires_introduction_and_fenced_json(self):
        self.prepare("wiki-json-example")
        body = '{"timeout_ms":750}'
        intro = "超时以毫秒计。"
        for label, text, expected in [
            ("valid", intro + "\n\n```json\n" + body + "\n```", True),
            ("raw", body, False),
            ("no-unit", "超时设置\n```json\n" + body + "\n```", False),
            ("wrong-type", intro + '\n```json\n{"timeout_ms":"750"}\n```', False),
        ]:
            result = verify(self.case, self.workspace, self.root / label, text)
            self.assertIsNone(result["error"])
            self.assertEqual(result["product_passed"], expected)

    def test_dispatch_state_selects_the_required_evidence(self):
        for case, correct, wrong in [("queue-paused", "暂停派送", "南二区"),
                                     ("queue-open", "南二区", "暂停派送")]:
            with self.subTest(case=case):
                self.prepare(case)
                for label, answer, passed in [("ok", correct, True), ("wrong", wrong, False)]:
                    result = verify(self.case, self.workspace, self.root / label, answer)
                    self.assertIsNone(result["error"])
                    self.assertEqual(result["product_passed"], passed)
                (self.workspace / "destination.txt").unlink()
                result = verify(self.case, self.workspace, self.root / "missing", correct)
                self.assertEqual([c["id"] for c in result["checks"] if c["status"] == "failed"], ["scope"])

    def test_rollout_requires_correct_branch_status_and_scoped_edit(self):
        for branch, status in [("apply", "已切换"), ("done", "已就绪"), ("conflict", "版本冲突")]:
            with self.subTest(branch=branch):
                self.prepare("rollout-" + branch)
                config = self.workspace / "deploy.ini"
                initial = config.read_text()
                final = initial.replace("[production]\nrelease=r14", "[production]\nrelease=r15")

                def failures(label, answer):
                    result = verify(self.case, self.workspace, self.root / label, answer)
                    self.assertIsNone(result["error"])
                    return {c["id"] for c in result["checks"] if c["status"] == "failed"}

                if branch == "apply":
                    self.assertEqual(failures("claimed-only", status), {"config"})
                config.write_text(final)
                self.assertFalse(failures("correct", status))
                self.assertEqual(failures("wrong-status", "完成"), {"decision"})
                config.write_text(final.replace("release=r14", "release=r15", 1))
                self.assertEqual(failures("staging-edited", status), {"config"})
                config.write_text(final)
                (self.workspace / "deploy.ini.bak").write_text(initial)
                self.assertEqual(failures("backup-left", status), {"scope"})

    def test_invoice_requires_both_deliveries_and_preserves_payments(self):
        self.prepare("invoice-close")
        balance = self.workspace / "balance.csv"
        balance.write_text("order,due,paid,balance\nA,100,35,65\nB,50,60,-10\nC,20,0,20\n")
        self.assertEqual(self.failed("partial"), {"exceptions"})
        (self.workspace / "exceptions.csv").write_text("payment_id,reason\np4,currency\np5,unknown_order\n")
        self.assertFalse(self.failed("complete"))
        balance.write_text(balance.read_text().replace("35,65", "70,30"))
        (self.workspace / "payments.csv").unlink()
        self.assertEqual(self.failed("double-count"), {"balance", "scope"})

    def test_latest_query_runs_on_unseen_data_and_requires_read_only_sql(self):
        self.prepare("latest-sample-sql")
        path = self.workspace / "latest.sql"
        sql = ("SELECT id AS device, (SELECT value FROM samples WHERE device=id AND valid=1 "
               "ORDER BY tick DESC, seq DESC LIMIT 1) AS value FROM devices WHERE enabled=1 ORDER BY id")
        path.write_text(sql)
        self.assertFalse(self.failed("correct"))
        for label, query in [("wrong-order", sql.replace("tick DESC, ", "")),
                             ("includes-invalid", sql.replace(" AND valid=1", "")),
                             ("wrong-columns", sql.replace("AS device", "AS name")),
                             ("empty", "-- pending"), ("mutates", "DELETE FROM samples")]:
            path.write_text(query)
            self.assertEqual(self.failed(label), {"query"})

    def test_rail_rebooking_identifies_the_failed_stage(self):
        self.prepare("rail-rebooking")
        expected = self.case["paths"]["fixture"].parent / "expected"
        shutil.copytree(expected, self.workspace, dirs_exist_ok=True)
        self.assertFalse(self.failed("complete"))
        for table, old, new in [
            ("snapshot", ",conflict", ",live"),
            ("decisions", ",second,", ",first,"),
            ("capacity-final", ",", ";"),
            ("reconciliation", "wait,32,112", "wait,32,111"),
        ]:
            with self.subTest(table=table):
                path = self.workspace / (table + ".csv")
                original = path.read_bytes()
                self.assertIn(old.encode(), original)
                path.write_bytes(original.replace(old.encode(), new.encode(), 1))
                self.assertEqual(self.failed(table), {table})
                path.write_bytes(original)
        (self.workspace / "requests.csv").unlink()
        (self.workspace / "scratch.py").touch()
        self.assertEqual(self.failed("collateral"), {"scope"})

    def test_edit_recovery_requires_exact_file(self):
        self.prepare("edit-recovery")
        path = self.workspace / "settings.ini"
        initial = path.read_bytes()
        self.assertEqual(self.failed("unchanged"), {"edit"})
        final = b"# generated v2\n" + initial.replace(b"sync_window=legacy", b"sync_window=current")
        path.write_bytes(final)
        self.assertFalse(self.failed("recovered"))
        path.write_bytes(final.replace(b"channel_0001=normal", b"channel_0001=current"))
        self.assertEqual(self.failed("collateral"), {"edit"})
