import json
from pathlib import Path
import sys

workspace, output = map(Path, sys.argv[1:])
try:
    answer = json.loads((output / "answer.txt").read_text())
except ValueError:
    answer = None
expected = {"incident": "C-71", "owner": "Rao", "next_check": "09:45"}
archive = workspace / "handoff.txt"
scope = ({p.name for p in workspace.iterdir() if p.name != ".git"} == {archive.name}
         and not archive.is_symlink() and archive.is_file()
         and archive.read_bytes() == (Path(__file__).with_name("fixture") / archive.name).read_bytes())
checks = [{"id": "handoff", "status": "passed" if answer == expected else "failed",
           "evidence": "Compared against the latest timestamp in the supplied handoff"},
          {"id": "scope", "status": "passed" if scope else "failed", "evidence": "Compared archive bytes and entries"}]
(output / "result.json").write_text(json.dumps({"schema_version": 1, "checks": checks}))
