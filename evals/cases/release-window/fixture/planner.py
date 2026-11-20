"""Prototype used by the release desk. Minutes are relative to shift start."""
import json
from pathlib import Path
import sys


def main():
    bundle, out = map(Path, sys.argv[1:])
    plan = json.loads((bundle / "plan.json").read_text())
    queue = []
    clock = 0
    for job in plan["jobs"]:
        queue.append({"id": job["id"], "start": clock, "end": clock + job["minutes"]})
        clock += job["minutes"]
    out.write_text(json.dumps({"schedule": queue}))


if __name__ == "__main__":
    main()
