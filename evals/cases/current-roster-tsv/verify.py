from pathlib import Path
import json
import sys

workspace, output = map(Path, sys.argv[1:])
# Accept a conventional final line break, but retain spaces, extra rows and wrappers.
rows = (output / "answer.txt").read_text().removesuffix("\n").split("\n")
correct = rows == ["team\towner", "navy\t杜", "plum\t周"]
roster = workspace / "roster.csv"
intact = ({p.name for p in workspace.iterdir() if p.name != ".git"} == {roster.name}
          and not roster.is_symlink() and roster.is_file()
          and roster.read_bytes() == (Path(__file__).with_name("fixture") / roster.name).read_bytes())
checks = [{"id": "tsv", "status": "passed" if correct else "failed", "evidence": "Compared raw TSV header and records"},
          {"id": "scope", "status": "passed" if intact else "failed", "evidence": "Compared roster bytes and entries"}]
(output / "result.json").write_text(json.dumps({"schema_version": 1, "checks": checks}))
