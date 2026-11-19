import csv
import json
from pathlib import Path
import sys

workspace, output = map(Path, sys.argv[1:])
fixture = Path(__file__).with_name("fixture")
expected = [
    ["site", "start", "end"],
    ["east", "08:30", "09:00"],
    ["west", "10:00", "11:00"],
    ["west", "13:00", "14:00"],
]
target = workspace / "windows.csv"
rows = []
if target.is_file() and not target.is_symlink():
    try:
        with target.open(newline="") as stream:
            rows = list(csv.reader(stream, strict=True))
    except (csv.Error, UnicodeError):
        pass
entries = {p.name for p in workspace.iterdir() if p.name != ".git"}
scope = entries == {"shifts.csv", "coverage.csv", "windows.csv"} and all(
    (workspace / seed.name).is_file() and not (workspace / seed.name).is_symlink()
    and (workspace / seed.name).read_bytes() == seed.read_bytes() for seed in fixture.iterdir()
)
checks = [{"id": key, "status": "passed" if ok else "failed", "evidence": evidence}
          for key, ok, evidence in [("windows", rows == expected, "Checked all ordered CSV fields"),
                                    ("scope", scope, "Checked source bytes and root entries")]]
(output / "result.json").write_text(json.dumps({"schema_version": 1, "checks": checks}))
