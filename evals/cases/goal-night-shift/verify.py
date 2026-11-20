from functools import partial
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
import json
from pathlib import Path
import subprocess
import sys
from threading import Thread


def progression(rows):
    task = None
    checkpoints = 0
    worked = review = False
    for event in rows:
        kind, source = event["type"], event.get("source", {})
        if kind == "run_started" and source == {"kind": "headless", "id": "task"}:
            if task and task != event["run_id"]:
                return False, "Multiple headless task runs in trace"
            task = event["run_id"]
        if not task or event.get("run_id") != task:
            continue
        data = event.get("payload") or {}
        if kind == "tool_completed":
            executions = (data.get("evidence") or {}).get("executions") or []
            if (data.get("goal_progress") is not True or data.get("is_error")
                    or not executions or executions[-1].get("status") != "success"):
                continue
            worked = True
        elif kind == "system_message" and source.get("kind") == "goal":
            if data.get("goal_continuation"):
                if not worked:
                    return False, "Goal continuation without successful work in the preceding phase"
                review = False
                checkpoints += 1
                worked = False
                continue
            review = worked and checkpoints >= 2 and data.get("content", "").startswith("Goal awaits your review.")
    return checkpoints >= 2 and worked and review, f"Goal checkpoints with tool activity: {checkpoints}; final phase activity: {worked}; runtime review: {review}"


def main():
    case = Path(__file__).parent
    workspace, output = map(Path, sys.argv[1:3])
    trace = Path(sys.argv[3]) if len(sys.argv) > 3 else None
    status, evidence = "not_run", "Runtime trace unavailable"
    if trace and trace.is_file():
        with trace.open() as stream:
            passed, evidence = progression(json.loads(line) for line in stream)
        status = "passed" if passed else "failed"
    target = workspace / "levels.json"
    unchanged = target.is_file() and not target.is_symlink() and target.read_bytes() == (case / "fixture/levels.json").read_bytes()
    checks = [{"id": "progression", "status": status, "evidence": evidence},
              {"id": "maps", "status": "passed" if unchanged else "failed", "evidence": "Compared all published level bytes"}]
    with ThreadingHTTPServer(("127.0.0.1", 0), partial(SimpleHTTPRequestHandler, directory=workspace)) as server:
        thread = Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            subprocess.run(["node", str(case / "browser.cjs"), f"http://127.0.0.1:{server.server_port}/", str(output)], timeout=90, check=True)
        finally:
            server.shutdown()
            thread.join()
    checks.extend(json.loads((output / "browser.json").read_text()))
    (output / "result.json").write_text(json.dumps({"schema_version": 1, "checks": checks}))


if __name__ == "__main__":
    main()
