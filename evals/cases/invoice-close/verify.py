import csv
import json
from pathlib import Path
import sys

workspace, output = map(Path, sys.argv[1:])
expected = {
    "balance": [["order", "due", "paid", "balance"], ["A", "100", "35", "65"],
                ["B", "50", "60", "-10"], ["C", "20", "0", "20"]],
    "exceptions": [["payment_id", "reason"], ["p4", "currency"], ["p5", "unknown_order"]],
}
checks = []
for key, rows in expected.items():
    path = workspace / (key + ".csv")
    ok = False
    if path.is_file() and not path.is_symlink():
        try:
            with path.open(newline="") as stream:
                ok = list(csv.reader(stream, strict=True)) == rows
        except (UnicodeError, csv.Error):
            pass
    checks.append({"id": key, "status": "passed" if ok else "failed", "evidence": path.name})

fixture = Path(__file__).with_name("fixture")
inputs = {p.name for p in fixture.iterdir()}
scope = {p.name for p in workspace.iterdir() if p.name != ".git"} <= inputs | {'exceptions.csv', 'balance.csv'}
for name in inputs:
    path = workspace / name
    scope = scope and not path.is_symlink() and path.is_file() and path.read_bytes() == (fixture / name).read_bytes()
checks.append({"id": "scope", "status": "passed" if scope else "failed", "evidence": "Compared workspace with fixture"})
(output / "result.json").write_text(json.dumps({"schema_version": 1, "checks": checks}))
