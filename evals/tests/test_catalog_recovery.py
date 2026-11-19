import json
from pathlib import Path
import shutil
import tempfile
import unittest

from agent_eval.catalog import load_case
from agent_eval.verification import verify


class CatalogRecoveryTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.case = load_case("catalog-recovery")
        self.product = self.root / "product"
        shutil.copytree(self.case["paths"]["fixture"], self.product)

    def statuses(self, label):
        result = verify(self.case, self.product, self.root / label)
        self.assertIsNone(result["error"])
        return {item["id"]: item["status"] for item in result["checks"]}

    def test_dead_cli_cannot_earn_rollback_credit(self):
        (self.product / "catalog.py").write_text("raise SystemExit(1)\n")
        self.assertEqual(set(self.statuses("dead").values()), {"failed"})

    def test_recovered_archive_requires_original_bytes(self):
        expected = {"version": 2, "records": {"red": {"rev": 3, "name": None}, "blue": {"rev": 5, "name": "靛蓝"}}}
        archive = self.product / "archive"
        (archive / "recovered.json").write_text(json.dumps(expected))
        self.assertEqual(self.statuses("delivered")["archive"], "passed")
        with (archive / "catalog.json").open("a") as f:
            f.write("\n")
        self.assertEqual(self.statuses("modified")["archive"], "failed")

    def test_reference_cli_and_old_reader_probe(self):
        import subprocess
        import sys

        oracle = Path(__file__).parent / "oracles/catalog-recovery/catalog.py"
        target = self.product / "catalog.py"
        shutil.copyfile(oracle, target)
        archive = self.product / "archive"
        shutil.copyfile(archive / "catalog.json", archive / "recovered.json")
        subprocess.run([sys.executable, target, archive / "recovered.json", archive / "recovery.jsonl"], check=True)
        self.assertEqual(set(self.statuses("reference").values()), {"passed"})
        # Same final JSON, but an in-place publisher corrupts the old reader's view.
        target.write_text(target.read_text().replace("os.replace(temp.name, snapshot)",
                                                     "snapshot.write_bytes(Path(temp.name).read_bytes())"))
        failures = {key for key, status in self.statuses("in-place").items() if status == "failed"}
        self.assertEqual(failures, {"publish"})
