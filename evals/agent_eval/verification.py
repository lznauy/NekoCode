import json
import os
from pathlib import Path
import signal
import subprocess
import shutil
import tempfile

from .storage import tree_hash, write_json
from . import verifier_sandbox


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


def verify(case, workspace, output, final_text="", input_sha256=None, events_path=None, original_workspace=None, workspace_change_ns=None):
    workspace, output = Path(workspace).resolve(), Path(output).resolve()
    if output.is_relative_to(workspace) or workspace.is_relative_to(output):
        raise ValueError('verification input and output must not overlap')
    output.mkdir(parents=True, exist_ok=False)
    (output / "answer.txt").write_text(final_text)
    info = {}
    if workspace_change_ns is not None:
        info["workspace_change_ns"] = workspace_change_ns
    if input_sha256 is not None:
        info["sha256"] = input_sha256
        if "architecture" in case["manifest"]["tags"]:
            info["unchanged"] = tree_hash(workspace) == input_sha256
    if info:
        write_json(output / "input.json", info)
    result_path = output / "result.json"
    # The sandbox gets a private output directory. Never let a candidate plant
    # links or device files in the host directory used for normalized reports.
    with tempfile.TemporaryDirectory(prefix="nekocode-verifier-output-") as temp:
        staging = Path(temp)
        for name in ("answer.txt", "input.json"):
            if (output / name).is_file():
                shutil.copyfile(output / name, staging / name)
        error = None
        try:
            args = verifier_sandbox.command(case, workspace, staging, events_path, original_workspace)
            # Only Bubblewrap itself sees this environment; --clearenv sets up the guest.
            with (output / "stdout.log").open("w") as stdout, (output / "stderr.log").open("w") as stderr:
                proc = subprocess.Popen(args, cwd=staging, stdout=stdout, stderr=stderr, env={},
                                        stdin=subprocess.DEVNULL, start_new_session=True)
                try:
                    code = proc.wait(timeout=case["manifest"]["budgets"]["verifier_seconds"])
                    if code:
                        error = f"isolated verifier exit {code}; inspect stderr.log"
                except subprocess.TimeoutExpired:
                    error = "verifier timeout"
                finally:
                    stop_process(proc)
            if not error:
                for name in ("result.json", "render.json", "desktop.png", "mobile.png", "product.png"):
                    source = staging / name
                    if source.is_symlink() or (source.exists() and not source.is_file()):
                        raise ValueError("invalid verifier output: " + name)
                    if source.is_file():
                        shutil.copyfile(source, output / name)
        except (OSError, ValueError, subprocess.SubprocessError) as exc:
            error = "verifier sandbox setup/export failed: " + str(exc)
    try:
        if error:
            raise ValueError(error)
        result = json.loads(result_path.read_text())
        expected = {r["id"] for r in case["manifest"]["requirements"]}
        checks = result["checks"]
        if result.get("schema_version") != 1 or not isinstance(checks, list) or len(checks) != len(expected):
            raise ValueError("invalid verification schema")
        if {c["id"] for c in checks} != expected or any(c.get("status") not in {"passed", "failed", "not_run"} or not isinstance(c.get("evidence"), str) for c in checks):
            raise ValueError("verification requirements/status mismatch")
        if result.get("error"):
            raise ValueError(result["error"])
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
