import importlib.util
from pathlib import Path
import tempfile
import unittest


def load_delivery():
    source = Path(__file__).resolve().parents[1] / "cases/relay-station/delivery_checks.py"
    spec = importlib.util.spec_from_file_location("relay_delivery", source)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


delivery = load_delivery()


class RelayDeliveryTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name) / "product"
        for folder in ("docs", "relay", "tests"):
            (self.root / folder).mkdir(parents=True)

    def put(self, name, content):
        path = self.root / name
        path.write_text(content)
        return path

    def test_code_line_count_filters_documentation_and_formatting(self):
        sample = self.put("relay/main.py", '"""Docs\ntext\n"""\n# note\n\nitems = [\n  1,\n  2\n]\n')
        self.assertEqual(delivery.code_lines(sample), 3)
        with self.assertRaisesRegex(ValueError, "relay=3, tests=0"):
            delivery.scale(self.root)
        self.put("tests/test_demo.py", "# filler\n" * 1100)
        with self.assertRaisesRegex(ValueError, "total=3"):
            delivery.scale(self.root)
        self.put("relay/logic.py", "".join(f"v{i} = {i}\n" for i in range(500)))
        self.put("tests/test_demo.py", "".join(f"v{i} = {i}\n" for i in range(497)))
        self.assertIn("total=1000", delivery.scale(self.root))

    def test_design_matches_live_files_and_rejects_missing_references(self):
        self.put("README.md", "python3 -m relay\npython3 -m unittest discover -s tests")
        paths = ["relay/cli.py", "relay/db.py", "relay/routing.py", "tests/test_db.py", "tests/test_cli.py"]
        for path in paths:
            self.put(path, "")
        self.put("docs/design.md", "SQLite transactions and retries, test plan.\n" +
                 "\n".join(f"[module](../{path})" for path in paths))
        self.assertIn("relay/routing.py", delivery.design(self.root))
        (self.root / paths[-1]).unlink()
        with self.assertRaisesRegex(ValueError, "missing regular file"):
            delivery.design(self.root)
        (self.root / paths[-1]).symlink_to(__file__)
        with self.assertRaisesRegex(ValueError, "invalid project link"):
            delivery.design(self.root)

    def test_real_suite_catches_independent_mutation_only_with_behavioral_assertion(self):
        self.put("relay/__main__.py", 'import json\nprint(json.dumps({"minutes": 6, "ok": True}))\n')
        header = """import json, subprocess, sys, unittest

class CliTests(unittest.TestCase):
    def test_route(self):
        reply = subprocess.run([sys.executable, "-m", "relay", "--db", "demo.db", "route"],
                               capture_output=True, check=True)
        self.assertEqual(json.loads(reply.stdout)["minutes"], 6)
"""
        tests = self.put("tests/test_cli.py", header + "".join(
            f"    def test_other_{n}(self): self.assertEqual({n}, {n})\n" for n in range(11)))
        before = (self.root / "relay/__main__.py").read_text()
        self.assertIn("12 passing tests", delivery.project_tests(self.root))
        self.assertEqual(before, (self.root / "relay/__main__.py").read_text())
        tests.write_text(tests.read_text().replace('self.assertEqual(json.loads(reply.stdout)["minutes"], 6)',
                                                 'self.assertTrue(json.loads(reply.stdout)["ok"])'))
        with self.assertRaisesRegex(ValueError, "needs a failing assertion"):
            delivery.project_tests(self.root)

    def test_skip_only_and_empty_suites_fail(self):
        self.put("relay/__main__.py", "")
        for content in ("", "import unittest\n@unittest.skip('pending')\nclass T(unittest.TestCase):\n" +
                        "".join(f" def test_{n}(self): pass\n" for n in range(12))):
            self.put("tests/test_cli.py", content)
            with self.assertRaisesRegex(ValueError, "needs 12 passing"):
                delivery.project_tests(self.root)

    def test_copy_rejects_links(self):
        (self.root / "tests/escape").symlink_to(self.root.parent)
        with self.assertRaisesRegex(ValueError, "non-regular entry"):
            delivery.copy_product(self.root, self.root.parent / "staged")
