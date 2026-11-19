import json
from pathlib import Path
import sys

workspace, output = map(Path, sys.argv[1:])
try:
    answer = json.loads((output / "answer.txt").read_text())
except ValueError:
    answer = None
shape = isinstance(answer, dict) and set(answer) == {"M14", "M19"}
fixture = Path(__file__).with_name("fixture")
files = {p.name for p in fixture.iterdir()}
scope = ({p.name for p in workspace.iterdir() if p.name != ".git"} == files and
         all(not (workspace / name).is_symlink() and (workspace / name).is_file() and
             (workspace / name).read_bytes() == (fixture / name).read_bytes() for name in files))
criteria = [
    ("positive", shape and answer["M19"] == {"status": "approved", "receipt": "S37"}, "Approved current revision"),
    ("gap", shape and answer["M14"] == {"status": "unconfirmed", "receipt": None}, "No matching approved receipt"),
    ("scope", scope, "Compared input bytes and workspace entries"),
]
checks = [{"id": key, "status": "passed" if ok else "failed", "evidence": evidence}
          for key, ok, evidence in criteria]
(output / "result.json").write_text(json.dumps({"schema_version": 1, "checks": checks}))
