import json
from pathlib import Path
import sqlite3
import sys

workspace, output = map(Path, sys.argv[1:])
query = workspace / "latest.sql"
fixture = Path(__file__).with_name("fixture")
datasets = [
    ([("z", 1), ("a", 1), ("b", 0), ("m", 1)],
     [(1, "z", 4, 12, 1), (2, "z", 4, 0, 1), (3, "z", 5, 11, 0),
      (4, "a", 9, 8, 0), (5, "b", 8, 5, 1)],
     [("a", None), ("m", None), ("z", 0)]),
    ([("x", 1), ("d", 1)], [(8, "d", 3, 4, 1), (2, "d", 8, -6, 1),
                              (1, "x", 2, 7, 1)], [("d", -6), ("x", 7)]),
]
ok = query.is_file() and not query.is_symlink()
if ok:
    try:
        sql = query.read_text()
        for devices, samples, expected in datasets:
            with sqlite3.connect(":memory:") as db:
                db.executescript((fixture / "schema.sql").read_text())
                db.executemany("INSERT INTO devices VALUES (?,?)", devices)
                db.executemany("INSERT INTO samples VALUES (?,?,?,?,?)", samples)
                db.set_authorizer(lambda action, *_: sqlite3.SQLITE_OK if action in
                                  (sqlite3.SQLITE_SELECT, sqlite3.SQLITE_READ, sqlite3.SQLITE_FUNCTION,
                                   sqlite3.SQLITE_RECURSIVE) else sqlite3.SQLITE_DENY)
                db.set_progress_handler(lambda: 1, 100000)
                result = db.execute(sql)
                ok = ([c[0] for c in (result.description or ())] == ["device", "value"]
                      and result.fetchmany(len(expected) + 1) == expected)
                db.set_authorizer(None)
            if not ok:
                break
    except (UnicodeError, sqlite3.Error):
        ok = False
checks = [{"id": "query", "status": "passed" if ok else "failed", "evidence": "Executed against independent datasets"}]

inputs = {p.name for p in fixture.iterdir()}
scope = {p.name for p in workspace.iterdir() if p.name != ".git"} <= inputs | {'latest.sql'}
for name in inputs:
    path = workspace / name
    scope = scope and not path.is_symlink() and path.is_file() and path.read_bytes() == (fixture / name).read_bytes()
checks.append({"id": "scope", "status": "passed" if scope else "failed", "evidence": "Compared workspace with fixture"})
(output / "result.json").write_text(json.dumps({"schema_version": 1, "checks": checks}))
