"""Black-box probes for the public relay contract. Each request starts a fresh process."""
from concurrent.futures import ThreadPoolExecutor
import itertools
import json
from pathlib import Path
import random
import sqlite3
import subprocess
import sys
import tempfile
import threading


def require(condition, message):
    if not condition:
        raise AssertionError(message)


def expect(actual, wanted, where):
    """Compare all required fields. The public contract permits extra response fields."""
    if isinstance(wanted, dict):
        require(isinstance(actual, dict), f"{where}: expected object, got {actual!r}")
        for key, value in wanted.items():
            require(key in actual, f"{where}: missing {key}")
            expect(actual[key], value, f"{where}.{key}")
    elif isinstance(wanted, list):
        require(isinstance(actual, list) and len(actual) == len(wanted),
                f"{where}: expected {wanted!r}, got {actual!r}")
        for index, (a, w) in enumerate(zip(actual, wanted)):
            expect(a, w, f"{where}[{index}]")
    else:
        require(type(actual) is type(wanted) and actual == wanted,
                f"{where}: expected {wanted!r}, got {actual!r}")


class Client:
    def __init__(self, workspace, db):
        self.workspace, self.db = workspace, db

    def raw(self, operation, text):
        result = subprocess.run(
            [sys.executable, "-B", "-m", "relay", "--db", str(self.db), operation],
            input=text, text=True, cwd=self.workspace, capture_output=True, timeout=8)
        try:
            reply = json.loads(result.stdout)
        except ValueError as exc:
            raise AssertionError(f"{operation}: exit {result.returncode}, invalid JSON; "
                                 f"stderr {result.stderr[-250:]!r}") from exc
        return result.returncode, reply

    def call(self, operation, error=None, **data):
        status, reply = self.raw(operation, json.dumps(data, ensure_ascii=False))
        wanted_status = 2 if error else 0
        require(status == wanted_status, f"{operation}: expected exit {wanted_status}, got {status}: {reply!r}")
        expect(reply, {"ok": False, "error": error} if error else {"ok": True}, operation)
        return reply


def link(a, b, minutes=1, capacity=1):
    return {"from": a, "to": b, "minutes": minutes, "capacity": capacity}


def load(client, stations, links):
    client.call("import", stations=stations, links=links)


def path(client, origin, destination, stations, minutes):
    result = client.call("route", origin=origin, destination=destination)
    expect(result, {"path": stations, "minutes": minutes}, f"route {origin} -> {destination}")


def network(client):
    seed = link("西", "北", 3)
    load(client, ["西", "北"], [seed])
    load(client, ["西", "北"], [seed])
    for bad, code in [(link("南", "缺"), "unknown_station"), (link("西", "北", 4), "conflict"),
                      (link("南", "南"), "invalid"), (link("西", "南", True), "invalid"),
                      (link("北", "南", capacity=0), "invalid")]:
        client.call("import", error=code, stations=["南"], links=[link("南", "北"), bad])
        client.call("route", error="unknown_station", origin="南", destination="北")
        path(client, "西", "北", ["西", "北"], 3)
    status, result = client.raw("import", "{")
    require(status == 2, "malformed JSON did not exit 2")
    expect(result, {"ok": False, "error": "invalid"}, "malformed JSON")
    path(client, "北", "北", ["北"], 0)
    client.call("route", error="unreachable", origin="北", destination="西")
    with client.db.open("rb") as f:
        require(f.read(16) == b"SQLite format 3\x00", "database is not SQLite")
    with sqlite3.connect(f"{client.db.as_uri()}?mode=ro", uri=True) as db:
        require(db.execute("PRAGMA integrity_check").fetchone() == ("ok",), "SQLite integrity_check failed")


def shortest(stations, links, start, end):
    """Small graphs permit exhaustive simple-path enumeration, independent of graph algorithm."""
    if start == end:
        return 0, [start]
    weights = {(edge["from"], edge["to"]): edge["minutes"] for edge in links}
    other = sorted(set(stations) - {start, end})
    candidates = []
    for length in range(len(other) + 1):
        for middle in itertools.permutations(other, length):
            nodes = (start, *middle, end)
            hops = list(zip(nodes, nodes[1:]))
            if all(hop in weights for hop in hops):
                candidates.append((sum(weights[hop] for hop in hops), len(hops), nodes))
    if candidates:
        cost, _, chosen = min(candidates)
        return cost, list(chosen)
    return None


def routing(client):
    # Equal time/hops, smaller final predecessor on the lexicographically *larger* path.
    nodes = ["A", "B", "Z", "M", "C", "T", "Q"]
    links = [link("A", "C"), link("C", "M"), link("M", "T"), link("A", "B"), link("B", "Z"),
             link("Z", "T"), link("T", "A", 9), link("A", "Q", 4), link("T", "Q")]
    load(client, nodes, links)
    for dest in ("T", "Q"):
        cost, chosen = shortest(nodes, links, "A", dest)
        path(client, "A", dest, chosen, cost)
    # Independent weighted digraph, including cycles and isolated destinations.
    rng = random.Random(3807)
    nodes = ["港" + str(i) for i in range(6)]
    links = [link(a, b, rng.randint(1, 6)) for a in nodes for b in nodes if a != b and rng.random() < .34]
    load(client, nodes, links)
    for a, b in rng.sample(list(itertools.permutations(nodes, 2)), 12):
        chosen = shortest(nodes, links, a, b)
        if chosen is None:
            client.call("route", error="unreachable", origin=a, destination=b)
        else:
            path(client, a, b, chosen[1], chosen[0])


def parcel(client, op, pkg, path, state, station, **data):
    reply = client.call(op, id=pkg, **data)
    expect(reply, {"parcel": {"id": pkg, "path": path, "state": state, "station": station}}, op)
    return reply


def lifecycle(client):
    p = ["X", "Y", "Z"]
    load(client, p + ["I"], [link("X", "Y", 2), link("Y", "Z", 3)])
    for a, b, code in [("X", "X", "invalid"), ("I", "X", "unreachable"), ("X", "?", "unknown_station")]:
        client.call("create", error=code, id="pkg", origin=a, destination=b, key="a")
    parcel(client, "create", "pkg", p, "ready", "X", origin="X", destination="Z", key="a")
    client.call("create", error="conflict", id="pkg", origin="X", destination="Z", key="another")
    load(client, [], [link("X", "Z")])
    path(client, "X", "Z", ["X", "Z"], 1)
    client.call("dispatch", id="none", key="b", error="missing")
    client.call("arrive", id="pkg", key="b", error="state")
    parcel(client, "dispatch", "pkg", p, "in_transit", "X", key="b")
    client.call("dispatch", id="pkg", key="unused", error="state")
    parcel(client, "arrive", "pkg", p, "ready", "Y", key="c")
    parcel(client, "dispatch", "pkg", p, "in_transit", "Y", key="d")
    parcel(client, "arrive", "pkg", p, "delivered", "Z", key="e")
    client.call("arrive", id="pkg", key="repeat", error="state")
    parcel(client, "get", "pkg", p, "delivered", "Z")


def report(client, ready, transit, delivered, busy):
    expect(client.call("report"), {"counts": {"ready": ready, "in_transit": transit, "delivered": delivered},
                                   "busy": busy}, "report")


def ledger(client):
    load(client, ["东", "站"], [link("站", "东", capacity=2), link("东", "站")])
    report(client, 0, 0, 0, [])
    for pkg, origin, dest in [("a", "站", "东"), ("b", "东", "站"), ("c", "站", "东")]:
        client.call("create", id=pkg, key=pkg, origin=origin, destination=dest)
    for pkg in "abc":
        client.call("dispatch", id=pkg, key="d"+pkg)
    report(client, 0, 3, 0, [{"from": "东", "to": "站", "used": 1}, {"from": "站", "to": "东", "used": 2}])
    client.call("arrive", id="a", key="aa")
    report(client, 0, 2, 1, [{"from": "东", "to": "站", "used": 1}, {"from": "站", "to": "东", "used": 1}])
    events = [dict(seq=i, op=op, id=pkg, key=key) for i, (op, pkg, key) in enumerate([
        ("create", "a", "a"), ("create", "b", "b"), ("create", "c", "c"), ("dispatch", "a", "da"),
        ("dispatch", "b", "db"), ("dispatch", "c", "dc"), ("arrive", "a", "aa")], start=1)]
    for args, expected in [({}, events), ({"after": 2, "limit": 3}, events[2:5]),
                           ({"after": 1, "limit": 1, "id": "a"}, [events[3]]),
                           ({"after": 7}, []), ({"id":"absent"}, [])]:
        expect(client.call("ledger", **args), {"events": expected}, "ledger")
    for args in [{"limit": 0}, {"limit": 101}, {"after": -1}, {"after": True}]:
        client.call("ledger", error="invalid", **args)


def recovery(client):
    load(client, ["N", "S"], [link("N", "S")])
    sample = dict(id="first", key="book", origin="N", destination="S")

    def parallel(requests):
        barrier = threading.Barrier(len(requests), timeout=8)
        def invoke(request):
            barrier.wait()
            return client.raw(request[0], json.dumps(request[1]))
        with ThreadPoolExecutor(len(requests)) as pool:
            return list(pool.map(invoke, requests))

    first, *others = parallel([("create", sample)] * 5)
    require(first[0] == 0 and all(item == first for item in others), "concurrent duplicate create responses diverged")
    expect(first[1], {"ok": True, "parcel":{"id":"first","state":"ready","station":"N","path":["N","S"]}}, "concurrent create")
    client.call("create", id="second", key="another", origin="N", destination="S")
    ids = ["first", "second"]
    attempts = parallel([("dispatch", {"id": pkg, "key": "send-"+pkg}) for pkg in ids])
    require(sorted(result[0] for result in attempts) == [0, 2], "last capacity slot must admit exactly one process")
    win = next(i for i, result in enumerate(attempts) if result[0] == 0)
    winner, loser = ids[win], ids[1-win]
    expect(attempts[1-win][1], {"ok": False, "error": "capacity"}, "race loser")
    expect(attempts[win][1], {"ok": True, "parcel": {"id":winner, "state":"in_transit"}}, "race winner")
    report(client, 1, 1, 0, [{"from":"N","to":"S","used":1}])
    require(client.call("create", **dict(reversed(list(sample.items())))) == first[1], "replay lost first response")
    client.call("arrive", id=winner, key="book", error="conflict")
    client.call("create", **{**sample, "id":"changed"}, error="conflict")
    client.call("arrive", id=winner, key="finish")
    client.call("dispatch", id=loser, key="send-"+loser)
    client.call("arrive", id=loser, key="finish2")
    report(client, 0, 0, 2, [])
    expected = [("create","first","book"), ("create","second","another"), ("dispatch",winner,"send-"+winner),
                ("arrive",winner,"finish"), ("dispatch",loser,"send-"+loser), ("arrive",loser,"finish2")]
    events = [dict(seq=i, op=op, id=pkg, key=key) for i, (op, pkg, key)
              in enumerate(expected, start=1)]
    expect(client.call("ledger"), {"events": events}, "atomic journal")
    fresh = Client(client.workspace, client.db.with_name("standby.sqlite"))
    report(fresh, 0, 0, 0, [])
    with sqlite3.connect(f"{client.db.as_uri()}?mode=ro", uri=True) as source, sqlite3.connect(fresh.db) as target:
        source.backup(target)
    # Only SQLite data travels to the standby process, including original responses.
    report(fresh, 0, 0, 2, [])
    expect(fresh.call("ledger"), {"events": events}, "standby journal")
    require(fresh.call("create", **sample) == first[1], "standby lost original replay response")


def product_checks(workspace):
    results = []
    for scenario in (network, routing, lifecycle, ledger, recovery):
        with tempfile.TemporaryDirectory(prefix="relay-check-") as folder:
            try:
                scenario(Client(workspace, Path(folder) / "中转 1.sqlite"))
            except (AssertionError, OSError, sqlite3.Error, subprocess.SubprocessError, ValueError) as exc:
                status, evidence = "failed", f"{type(exc).__name__}: {exc}"[:650]
            else:
                status, evidence = "passed", f"{scenario.__name__} CLI scenario passed across fresh processes"
            results.append({"id": scenario.__name__, "status": status, "evidence": evidence})
    return results
