"""Independent legacy Conduit contract probes; no agent-produced tests trusted."""
import importlib.util
import json
from pathlib import Path
import sys
import unittest

workspace, output = Path(sys.argv[1]), Path(sys.argv[2])
# Freeze the evaluator source alongside the case; never resolve an agent file as a test.
sys.argv = [sys.argv[0], str(workspace)]
spec = importlib.util.spec_from_file_location("acceptance", Path(__file__).with_name("acceptance.py"))
module = importlib.util.module_from_spec(spec)
checks = []
try:
    spec.loader.exec_module(module)
    class Result(unittest.TextTestResult):
        def addSuccess(self, test):
            super().addSuccess(test)
        def stopTest(self, test):
            super().stopTest(test)
            bad = any(t == test or getattr(t, "test_case", None) == test for t, _ in self.failures + self.errors)
            checks.append({"id": test._testMethodName.removeprefix("test_"), "status": "failed" if bad else "passed", "evidence": "independent unittest: " + str(test)})
    unittest.TextTestRunner(verbosity=2, resultclass=Result).run(unittest.defaultTestLoader.loadTestsFromTestCase(module.Contract))
except Exception as exc:
    print(f"verifier setup failed: {exc}", file=sys.stderr)
    sys.exit(2)
(output / "result.json").write_text(json.dumps({"schema_version": 1, "checks": checks}))
