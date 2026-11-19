"""Reference CLI for the migration/replay acceptance tests."""
import json
import os
from pathlib import Path
import sys
import tempfile


def recover(snapshot, journal):
    raw = json.loads(snapshot.read_text(encoding="utf-8"))
    if raw["version"] == 1:
        records = {item["code"]: {"rev": item["revision"], "name": item["name"]} for item in raw["items"]}
    elif raw["version"] == 2:
        records = raw["records"]
    else:
        raise ValueError("unknown version")

    for line in journal.read_text(encoding="utf-8").splitlines():
        if not line.strip():
            continue
        event = json.loads(line)
        code, rev, op = event["code"], event["rev"], event["op"]
        if not isinstance(code, str) or not code or type(rev) is not int or rev <= 0:
            raise ValueError("invalid event key or revision")
        if op == "delete":
            name = None
        elif op == "put" and isinstance(event.get("name"), str) and event["name"]:
            name = event["name"]
        else:
            raise ValueError("invalid operation")
        previous = records.get(code)
        if previous is not None and rev <= previous["rev"]:
            if rev == previous["rev"] and name != previous["name"]:
                raise ValueError("conflict")
            continue
        records[code] = {"rev": rev, "name": name}

    with tempfile.NamedTemporaryFile(dir=snapshot.parent, delete=False) as temp:
        temp.write(json.dumps({"version": 2, "records": records}).encode("utf-8"))
    os.replace(temp.name, snapshot)


if __name__ == "__main__":
    recover(*map(Path, sys.argv[1:]))
