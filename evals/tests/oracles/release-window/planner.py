"""Reference product for verifier self-checks only."""
import csv
import json
from pathlib import Path
import sys


def positive(value):
    if type(value) is not int or value <= 0:
        raise ValueError("invalid integer")
    return value


def plan(bundle):
    jobs = {j["id"]: j for j in json.loads((bundle / "plan.json").read_text())["jobs"]}
    revisions = {}
    for line in (bundle / "changes.jsonl").read_text().splitlines():
        if not line.strip():
            continue
        e = json.loads(line)
        key, rev, duration = e["id"], positive(e["rev"]), positive(e["minutes"])
        if key not in jobs or revisions.setdefault((key, rev), duration) != duration:
            raise ValueError("invalid revision")
    for j in jobs.values():
        positive(j["minutes"])
    for (key, _), duration in sorted(revisions.items()):
        jobs[key]["minutes"] = duration
    with (bundle / "windows.csv").open(newline="") as stream:
        windows = sorted((w["crew"], int(w["start"]), int(w["end"])) for w in csv.DictReader(stream))
    rows, ends, crews = [], {}, {}
    while jobs:
        choices = []
        for key, j in jobs.items():
            if not all(dep in ends for dep in j["needs"]):
                continue
            start = max([crews.get(j["crew"], 0), *(ends[dep] for dep in j["needs"])])
            for crew, lo, hi in windows:
                if crew == j["crew"] and start < hi and start + j["minutes"] > lo:
                    start = hi
            choices.append((start, key))
        start, key = min(choices)
        j = jobs.pop(key)
        end = start + j["minutes"]
        rows.append(dict(id=key, start=start, end=end))
        ends[key] = crews[j["crew"]] = end
    return dict(schedule=rows, finish=max(ends.values(), default=0))


if __name__ == "__main__":
    result = plan(Path(sys.argv[1]))
    Path(sys.argv[2]).write_text(json.dumps(result))
