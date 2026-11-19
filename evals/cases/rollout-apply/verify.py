import json
from pathlib import Path
import sys

workspace, output = map(Path, sys.argv[1:])
case = Path(__file__).parent
config = workspace / "deploy.ini"
scope = {p.name for p in workspace.iterdir() if p.name != ".git"} == {config.name}
contents = (not config.is_symlink() and config.is_file()
            and config.read_bytes() == (case / "expected.ini").read_bytes())
answer = (output / "answer.txt").read_text().removesuffix("\n")
checks = []
for key, passed, evidence in [
    ("decision", answer == (case / "status.txt").read_text().rstrip("\n"), "Compared reported status"),
    ("config", contents, "Compared complete deployment configuration"),
    ("scope", scope, "Checked workspace entries"),
]:
    checks.append({"id": key, "status": "passed" if passed else "failed", "evidence": evidence})
(output / "result.json").write_text(json.dumps({"schema_version": 1, "checks": checks}))
