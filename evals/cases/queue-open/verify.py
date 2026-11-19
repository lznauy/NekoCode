import json
from pathlib import Path
import sys

workspace, output = map(Path, sys.argv[1:])
correct = (output / "answer.txt").read_text().removesuffix("\n") == "南二区"
fixture = Path(__file__).with_name("fixture")
expected_paths = {p.relative_to(fixture) for p in fixture.rglob("*")}
actual_paths = {p.relative_to(workspace) for p in workspace.rglob("*") if p.relative_to(workspace).parts[0] != ".git"}
scope = actual_paths == expected_paths and all(
    not (workspace / path).is_symlink() and
    ((workspace / path).is_dir() if (fixture / path).is_dir() else
     (workspace / path).is_file() and (workspace / path).read_bytes() == (fixture / path).read_bytes())
    for path in expected_paths
)
checks = [{"id": "decision", "status": "passed" if correct else "failed", "evidence": "Checked the result for this dispatch state"},
          {"id": "scope", "status": "passed" if scope else "failed", "evidence": "Workspace compared with the frozen fixture"}]
(output / "result.json").write_text(json.dumps({"schema_version": 1, "checks": checks}))
