from pathlib import Path
import shutil
import tempfile
import unittest

from agent_eval.catalog import load_case
from agent_eval.verification import verify


class ReleaseWindowTests(unittest.TestCase):
    def test_prototype_reference_and_partial_delivery(self):
        case = load_case("release-window")
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            product = root / "product"
            shutil.copytree(case["paths"]["fixture"], product)

            def failures(label, evidence=None):
                result = verify(case, product, root / label)
                self.assertIsNone(result["error"], result)
                for key, detail in (evidence or {}).items():
                    row = next(item for item in result["checks"] if item["id"] == key)
                    self.assertIn(detail, row["evidence"], row)
                return {row["id"] for row in result["checks"] if row["status"] != "passed"}

            self.assertEqual(failures("baseline"), {"handoff", "dependencies", "windows", "revisions", "rejection", "regression"})
            oracle = Path(__file__).parent / "oracles/release-window"
            shutil.copytree(oracle, product, dirs_exist_ok=True)
            self.assertEqual(failures("reference"), set())
            planner = product / "planner.py"
            source = planner.read_text()
            crew = 'crews.get(j["crew"], 0)'
            planner.write_text(source.replace(crew, f'({crew} if windows else 0)'))
            self.assertEqual(failures("crew", {"dependencies": "crew contention"}), {"dependencies"})
            planner.write_text(source.replace(crew, "0"))
            self.assertEqual(failures("all-crew", {"handoff": "schedule["}), {"handoff", "dependencies"})
            planner.write_text(source)
            tests = product / "tests/test_parallel.py"
            shape_only = tests.read_text().replace(
                'self.assertEqual([row["start"] for row in json.loads(out.read_text())["schedule"]], [0, 0])',
                'self.assertIn("finish", json.loads(out.read_text()))')
            self.assertNotEqual(shape_only, tests.read_text())
            tests.write_text(shape_only)
            self.assertEqual(failures("shape-only", {"regression": "serial"}), {"regression"})
            # A correct solver with stale handoff and vacuous tests earns partial credit.
            (product / "handoff.json").write_text("{}")
            (product / "tests/test_parallel.py").write_text("")
            self.assertEqual(failures("partial"), {"handoff", "regression"})
