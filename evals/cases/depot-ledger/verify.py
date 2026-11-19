"""Exercise the delivered CLI from separate processes against disposable databases."""
from concurrent.futures import ThreadPoolExecutor
import json
from pathlib import Path
import re
import sqlite3
import subprocess
import sys
import tempfile
import traceback

workspace, output = map(Path, sys.argv[1:])
case = Path(__file__).parent
legacy = (case / "fixture" / "legacy.sql").read_text()


def run(args, data=None, timeout=8):
    result = subprocess.run([sys.executable, *args], input=data, text=True, errors="replace",
                            capture_output=True, cwd=workspace, timeout=timeout)
    assert result.returncode == 0, result.stderr[-500:]
    return result


def api(db, op, error=None, **fields):
    result = run(["depot.py", str(db), "api"], json.dumps(dict(op=op, **fields)))
    msg = json.loads(result.stdout)
    assert msg["ok"] is (error is None), (op, msg)
    if error:
        assert msg["error"] == error, (op, msg)
    return msg


def line(site="north", sku="tea", qty=1):
    return dict(site=site, sku=sku, qty=qty)


def reserve(db, key, ticket, lines, **kw):
    return api(db, "reserve", key=key, id=ticket, expires=20, lines=lines, **kw)


def rows(db):
    return [(r["site"], r["sku"], r["on_hand"], r["held"], r["available"])
            for r in api(db, "stock")["rows"]]


def init(db):
    api(db, "init")


def events(db, after=0, limit=100):
    return [(r["seq"], r["op"], r["key"]) for r in api(db, "audit", after=after, limit=limit)["events"]]


def migration(db):
    with sqlite3.connect(db) as conn:
        conn.executescript(legacy)
        conn.execute("INSERT INTO balances VALUES ('east','茶',7)")
    init(db)
    init(db)
    assert rows(db) == [("east","茶",7,0,7), ("north","rice",4,0,4),
                        ("north","tea",12,0,12), ("south","tea",8,0,8)]
    api(db, "receive", key="add", **line("east","茶",2))
    assert run(["depot.py", str(db)]).stdout.splitlines() == [
        "east\t茶\t9", "north\trice\t4", "north\ttea\t12", "south\ttea\t8"]
    with sqlite3.connect(db) as conn:
        assert conn.execute("PRAGMA user_version").fetchone() == (2,)


def cli_path(db):
    # API and legacy readers must open the same operator-supplied filename.
    db = db.with_name("depot%20warehouse.db")
    init(db)
    api(db, "receive", key="path", **line(qty=4))
    assert run(["depot.py", str(db)]).stdout == "north\ttea\t4\n"


def bootstrap(db):
    init(db)
    assert rows(db) == [] and events(db) == []
    with sqlite3.connect(db) as conn:
        conn.executescript("CREATE TABLE marker AS SELECT 'safe' AS v; PRAGMA user_version=9;")
    api(db, "init", error="version")
    with sqlite3.connect(db) as conn:
        assert conn.execute("PRAGMA user_version").fetchone() == (9,)
        assert conn.execute("SELECT v FROM marker").fetchone() == ("safe",)


def allocation(db):
    init(db)
    api(db, "receive", key="r", **line(qty=5))
    parts = [line(qty=2), line(qty=2), line("east")]
    reserve(db, "q", "A", parts, error="shortage")
    reserve(db, "over", "B", [line(qty=3), line(qty=3)], error="shortage")
    assert rows(db) == [("north","tea",5,0,5)]
    api(db, "receive", key="e", **line("east"))
    reserve(db, "q", "A", parts)
    reserve(db, "q2", "A", [line()], error="conflict")
    assert rows(db) == [("east","tea",1,1,0), ("north","tea",5,4,1)]


def lifecycle(db):
    init(db)
    api(db, "receive", key="r", **line(qty=8))
    for idx, qty in enumerate([2, 3, 3]):
        reserve(db, str(idx), str(idx), [line(qty=qty)])
    api(db, "cancel", key="c", id="0")
    api(db, "ship", key="s", id="1")
    assert api(db, "sweep", key="early", now=19)["count"] == 0
    assert rows(db) == [("north","tea",5,3,2)]
    assert api(db, "sweep", key="at", now=20)["count"] == 1
    for idx in range(3):
        api(db, "ship", key="retry", id=str(idx), error="state")
    api(db, "cancel", key="bad", id="absent", error="missing")
    reserve(db, "last", "last", [line(qty=5)])
    api(db, "ship", key="ship-last", id="last")
    assert rows(db) == [("north","tea",0,0,0)]


def replay(db):
    init(db)
    req = dict(op="receive", key="arrive", **line(qty=5))
    first = api(db, **req)
    reserve(db, "q", "one", [line()])
    ans = api(db, "sweep", key="s", now=20)
    reserve(db, "next", "two", [line()])
    assert ans["count"] == 1 and api(db, "sweep", key="s", now=20) == ans
    assert json.loads(run(["depot.py", str(db), "api"], json.dumps(dict(reversed(list(req.items()))))).stdout) == first
    api(db, "cancel", key="arrive", id="two", error="conflict")
    assert rows(db) == [("north","tea",5,1,4)]


def audit(db):
    init(db)
    api(db, "receive", key="a", **line())
    api(db, "receive", key="a", **line())
    reserve(db, "fail", "X", [line(qty=2)], error="shortage")
    reserve(db, "b", "X", [line()])
    api(db, "ship", key="c", id="X")
    api(db, "sweep", key="d", now=0)
    assert events(db, 0, 2) == [(1,"receive","a"), (2,"reserve","b")]
    assert events(db, 2, 2) == [(3,"ship","c"), (4,"sweep","d")]
    assert events(db, 4) == []


def validation(db):
    init(db)
    for bad in (True, 0, -1, "3", 1.5):
        api(db, "receive", key="a", error="invalid", **line(qty=bad))
    for op, args in [("reserve", dict(key="b", id="n", expires=1, lines=[])),
                     ("receive", dict(key="", **line())), ("audit", dict(after=0, limit=101)),
                     ("sweep", dict(key="s", now=-1)), ("other", {})]:
        api(db, op, error="invalid", **args)
    msg = json.loads(run(["depot.py", str(db), "api"], "{").stdout)
    assert msg["ok"] is False and msg["error"] == "invalid"
    assert not rows(db) and not events(db)


def race(db):
    init(db)
    with ThreadPoolExecutor(6) as pool:
        def receive(_):
            return api(db, "receive", key="seed", **line(qty=4))
        results = list(pool.map(receive, range(6)))
        assert all(r == results[0] for r in results)
        def compete(i):
            payload = dict(op="reserve", key=str(i), id=str(i), expires=10, lines=[line(qty=3)])
            return json.loads(run(["depot.py", str(db), "api"], json.dumps(payload)).stdout)
        attempts = list(pool.map(compete, range(6)))
    assert sum(r["ok"] is True for r in attempts) == 1
    assert sum(r["ok"] is False and r.get("error") == "shortage" for r in attempts) == 5
    assert rows(db) == [("north","tea",4,3,1)] and len(events(db)) == 2


def delivery(db):
    doc = (workspace / "README.md").read_text()
    assert all(s in doc for s in ["depot.py", "init", "unittest"]), "Missing usage or test instructions"
    test = run(["-m", "unittest", "discover"], timeout=25)
    assert re.search(r"Ran [1-9]\d* tests? in ", test.stderr), "No discovered tests"


checks = []
with tempfile.TemporaryDirectory() as tmp:
    for check in (migration, cli_path, bootstrap, allocation, lifecycle, replay, audit, validation, race, delivery):
        try:
            check(Path(tmp) / (check.__name__ + ".db"))
            passed, evidence = True, "Public interface checks passed"
        except Exception as exc:
            frame = traceback.extract_tb(exc.__traceback__)[-1]
            reason = str(exc) or frame.line
            passed, evidence = False, f"{type(exc).__name__} in {frame.name}: {reason}"[:600]
        checks.append(dict(id=check.__name__, status="passed" if passed else "failed", evidence=evidence))
(output / "result.json").write_text(json.dumps(dict(schema_version=1, checks=checks)))
