import json
from pathlib import Path
import sys

workspace, output = map(Path, sys.argv[1:])
fixture = Path(__file__).with_name("fixture")
try:
    answer = json.loads((output / "answer.txt").read_text())
except ValueError:
    answer = {}
expected = {"test": "test_charge_completed_units", "actual_key": "reserved", "expected_key": "completed"}
files = {p.relative_to(workspace) for p in workspace.rglob("*")
         if (p.is_file() or p.is_symlink()) and not {".git", "__pycache__"}.intersection(p.relative_to(workspace).parts)}
seeds = {p.relative_to(fixture) for p in fixture.iterdir() if p.is_file()}
unchanged = files == seeds and all(not (workspace / p).is_symlink() and (workspace / p).read_bytes() == (fixture / p).read_bytes() for p in seeds)
checks = [{"id": name, "status": "passed" if ok else "failed", "evidence": evidence}
          for name, ok, evidence in [("diagnosis", answer == expected, "Failure method and both record keys"),
                                     ("unchanged", unchanged, "File list and contents compared with seed")]]
(output / "result.json").write_text(json.dumps({"schema_version": 1, "checks": checks}))
