import csv
import json
from pathlib import Path
import sys


def product_ok(workspace, fixture):
    source = workspace / "bookings.csv"
    report = workspace / "net.csv"
    if not all(p.is_file() and not p.is_symlink() for p in (source, report)):
        return False
    if source.read_bytes() != fixture.read_bytes():
        return False
    totals = {}
    with fixture.open(newline="") as rows:
        for row in csv.DictReader(rows):
            route = row["route"]
            delta = int(row["seats"]) * (1 if row["action"] == "reserve" else -1)
            totals[route] = totals.get(route, 0) + delta
    with report.open(encoding="utf-8", newline="") as rows:
        return list(csv.reader(rows)) == [["route", "net_seats"], *[
            [route, str(totals[route])] for route in sorted(totals)
        ]]


def lifecycle(events):
    task = next((e["run_id"] for e in events if e["type"] == "run_started"
                 and e.get("source") == {"kind": "headless", "id": "task"}), None)
    before = checkpoint = after = review = False
    for e in events:
        if not task or e.get("run_id") != task:
            continue
        kind, payload = e["type"], e.get("payload") or {}
        if kind == "tool_completed":
            executions = (payload.get("evidence") or {}).get("executions") or []
            if (payload.get("goal_progress") is not True or payload.get("is_error")
                    or not executions or executions[-1].get("status") != "success"):
                continue
            if checkpoint:
                after = True
            else:
                before = True
        elif kind == "system_message" and e.get("source", {}).get("kind") == "goal":
            if payload.get("goal_continuation"):
                review = False
                checkpoint = before
                continue
            review = payload.get("content", "").startswith("Goal awaits your review.")
    return after, review


def main():
    workspace, output = map(Path, sys.argv[1:3])
    trace = Path(sys.argv[3]) if len(sys.argv) > 3 else None
    recorded = trace is not None and trace.is_file()
    events = [json.loads(line) for line in trace.read_text().splitlines()] if recorded else []
    continued, review = lifecycle(events)
    try:
        delivered = product_ok(workspace, Path(__file__).parent / "fixture" / "bookings.csv")
    except (UnicodeError, csv.Error):
        delivered = False
    checks = []
    for key, passed, evidence in [
        ("continuation", continued, "Checked tool activity around the runtime-admitted Goal continuation in the task run"),
        ("delivery", delivered, "Compared input bytes and recomputed the sorted CSV from the frozen records"),
        ("review", review, "Checked the Goal runtime's review status in the task run"),
    ]:
        status = "passed" if passed else "failed"
        if not recorded and key != "delivery":
            status, evidence = "not_run", "Runtime trace unavailable"
        checks.append({"id": key, "status": status, "evidence": evidence})
    (output / "result.json").write_text(json.dumps({"schema_version": 1, "checks": checks}))


if __name__ == "__main__":
    main()
