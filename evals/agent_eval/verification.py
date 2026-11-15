import json
import os
from pathlib import Path
import signal
import subprocess
import sys

from .storage import write_json


def stop_process(proc):
    # The process group may outlive its leader. Never return only because the
    # leader exited; clean the group on success and failure alike.
    try:
        os.killpg(proc.pid, signal.SIGTERM)
    except ProcessLookupError:
        pass
    try:
        proc.wait(timeout=.3)
    except subprocess.TimeoutExpired:
        pass
    # A surviving grandchild can ignore TERM after its parent has exited.
    try:
        os.killpg(proc.pid, signal.SIGKILL)
    except ProcessLookupError:
        pass
    try:
        proc.wait(timeout=3)
    except subprocess.TimeoutExpired:
        pass


def verify(case, workspace, output, final_text="", input_sha256=None):
    workspace, output = Path(workspace).resolve(), Path(output).resolve()
    if output.is_relative_to(workspace) or workspace.is_relative_to(output):
        raise ValueError('verification input and output must not overlap')
    output.mkdir(parents=True, exist_ok=False)
    (output / "answer.txt").write_text(final_text)
    if input_sha256 is not None:
        write_json(output / "input.json", {"sha256": input_sha256})
    result_path = output / "result.json"
    env = {k: v for k, v in os.environ.items() if not any(x in k.upper() for x in ["API_KEY", "TOKEN", "SECRET", "PASSWORD"])}
    with (output / "stdout.log").open("w") as stdout, (output / "stderr.log").open("w") as stderr:
        proc = subprocess.Popen([sys.executable, str(case["paths"]["verifier"]), str(Path(workspace).resolve()), str(output.resolve())],
                                cwd=output, stdout=stdout, stderr=stderr, env=env, start_new_session=True)
        try:
            code = proc.wait(timeout=case["manifest"]["budgets"]["verifier_seconds"])
            error = None if code == 0 else f"verifier exit {code}"
        except subprocess.TimeoutExpired:
            stop_process(proc)
            error = "verifier timeout"
        finally:
            stop_process(proc)
    try:
        result = json.loads(result_path.read_text())
        expected = {r["id"] for r in case["manifest"]["requirements"]}
        checks = result["checks"]
        if result.get("schema_version") != 1 or not isinstance(checks, list) or len(checks) != len(expected):
            raise ValueError("invalid verification schema")
        if {c["id"] for c in checks} != expected or any(c.get("status") not in {"passed", "failed", "not_run"} or not isinstance(c.get("evidence"), str) for c in checks):
            raise ValueError("verification requirements/status mismatch")
        if error or result.get("error"):
            raise ValueError(error or result["error"])
        result["error"] = None
    except (OSError, ValueError, KeyError, TypeError) as exc:
        result = {"schema_version": 1, "error": error or str(exc), "checks": [
            {"id": r["id"], "status": "not_run", "evidence": "Verifier did not produce a valid result; inspect logs."}
            for r in case["manifest"]["requirements"]]}
    required = {r["id"] for r in case["manifest"]["requirements"] if r["required"]}
    result["required_passed"] = sum(c["id"] in required and c["status"] == "passed" for c in result["checks"])
    result["required_total"] = len(required)
    result["product_passed"] = not result["error"] and all(c["status"] == "passed" for c in result["checks"] if c["id"] in required)
    write_json(output / "verification.json", result)
    return result
