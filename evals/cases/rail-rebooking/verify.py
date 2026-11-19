import csv
import json
from pathlib import Path
import sys

workspace, output = map(Path, sys.argv[1:])
case = Path(__file__).parent
expected = sorted((case / "expected").glob("*.csv"))
checks = []
for table in expected:
    path = workspace / table.name
    evidence = "missing or invalid file"
    ok = False
    if not path.is_symlink() and path.is_file():
        try:
            with table.open(newline="") as reference, path.open(newline="") as actual:
                rows = list(csv.reader(actual, strict=True))
                wanted = list(csv.reader(reference))
            ok = rows == wanted
            first = next((i for i, pair in enumerate(zip(rows, wanted), 1) if pair[0] != pair[1]), None)
            evidence = f"{len(rows)}/{len(wanted)} rows including header"
            if first is not None:
                evidence += f", first difference at row {first}"
        except (UnicodeError, csv.Error) as error:
            evidence = str(error)
    checks.append({"id": table.stem, "status": "passed" if ok else "failed", "evidence": evidence})
inputs = list((case / "fixture").iterdir())
allowed = {p.name for p in inputs + expected}
scope = all(p.name in allowed for p in workspace.iterdir() if p.name != ".git")
for source in inputs:
    path = workspace / source.name
    scope = scope and not path.is_symlink() and path.is_file() and path.read_bytes() == source.read_bytes()
checks.append({"id": "scope", "status": "passed" if scope else "failed", "evidence": "Checked input bytes and workspace entries"})
(output / "result.json").write_text(json.dumps({"schema_version": 1, "checks": checks}))
