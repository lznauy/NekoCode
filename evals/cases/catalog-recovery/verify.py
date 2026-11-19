import json
from pathlib import Path
import subprocess
import sys
import tempfile

V1 = {"version": 1, "items": [{"code": "a", "name": "一", "revision": 3}]}
V2 = {"version": 2, "records": {"a": {"rev": 3, "name": "一"}}}


def run_scenario(workspace, root, scenario):
    snap, journal = root / "catalog.json", root / "events.jsonl"

    def seed(value):
        snap.write_text(json.dumps(value, indent=2) + "\n")

    def invoke(*events):
        journal.write_text("\n" + "\n".join(json.dumps(e) if isinstance(e, dict) else e for e in events))
        p = subprocess.run([sys.executable, str(workspace / "catalog.py"), str(snap), str(journal)],
                           cwd=root, capture_output=True, timeout=5)
        return p.returncode

    seed(V1 if scenario in ("migration", "conflict") else V2)
    put = dict(code="a", rev=4, op="put", name="新")
    delete = dict(code="a", rev=5, op="delete")
    if scenario == "migration":
        return invoke() == 0 and json.loads(snap.read_text()) == V2
    if scenario == "replay":
        other = dict(put, code="b", rev=1)
        for _ in range(2):
            if invoke(put, delete, dict(put, name="另一个旧值"), other, delete) != 0 or json.loads(snap.read_text()) != {
                "version": 2, "records": {"a": {"rev": 5, "name": None}, "b": {"rev": 1, "name": "新"}}}:
                return False
        return invoke(dict(put, rev=6)) == 0 and json.loads(snap.read_text())["records"] == {
            "a": {"rev": 6, "name": "新"}, "b": {"rev": 1, "name": "新"}}
    if scenario in ("conflict", "invalid"):
        # A broken CLI that always fails must not earn rollback credit.
        if invoke(put) != 0 or json.loads(snap.read_text()) != {"version": 2, "records": {"a": {"rev": 4, "name": "新"}}}:
            return False
        seed(V1 if scenario == "conflict" else V2)
    if scenario == "conflict":
        before = snap.read_bytes()
        return invoke(put, dict(put, op="delete")) != 0 and snap.read_bytes() == before
    if scenario == "invalid":
        for bad in ["{", dict(put, rev=True), dict(put, rev=0), dict(put, code=""), dict(put, op="skip"),
                    {"code": "a", "rev": 1, "op": "put"}, dict(put, rev=1, name="")]:
            before = snap.read_bytes()
            if invoke(put, bad) == 0 or snap.read_bytes() != before:
                raise ValueError(f"Malformed event accepted or input bytes changed: {bad!r}")
        seed({"version": 99, "records": {}})
        before = snap.read_bytes()
        return invoke(put) != 0 and snap.read_bytes() == before
    before = snap.read_bytes()
    with snap.open("rb") as reader:
        return invoke(put) == 0 and reader.read() == before and json.loads(snap.read_text()) == {
            "version": 2, "records": {"a": {"rev": 4, "name": "新"}}}


def evaluate(workspace, output):
    checks = []
    for key in ["archive", "migration", "replay", "conflict", "invalid", "publish"]:
        try:
            if key == "archive":
                archive = workspace / "archive"
                source = Path(__file__).parent / "fixture/archive"
                files = [archive / n for n in ["catalog.json", "recovery.jsonl", "recovered.json"]]
                passed = (all(f.is_file() and not f.is_symlink() and f.resolve().is_relative_to(workspace) for f in files)
                          and all(f.read_bytes() == (source / f.name).read_bytes() for f in files[:2])
                          and json.loads(files[2].read_text()) == {"version": 2, "records": {
                              "red": {"rev": 3, "name": None}, "blue": {"rev": 5, "name": "靛蓝"}}})
            else:
                with tempfile.TemporaryDirectory() as directory:
                    passed = run_scenario(workspace, Path(directory), key)
            descriptions = {
                "archive": "Original archive bytes and recovered records",
                "migration": "v1 snapshot and whitespace-only journal become the equivalent v2 snapshot",
                "replay": "Refresh, delete, contradictory stale put, independent key, duplicate batch and revival",
                "conflict": "Conflicting current revision rejected after a valid event; v1 bytes preserved",
                "invalid": "Malformed/invalid events and unknown snapshot version rejected without changes",
                "publish": "An already-open descriptor still reads the old bytes after successful publication",
            }
            evidence = f"{descriptions[key]}: {'passed' if passed else 'failed'}"
        except (OSError, ValueError, KeyError, TypeError, subprocess.TimeoutExpired) as exc:
            passed, evidence = False, str(exc)[:300]
        checks.append(dict(id=key, status="passed" if passed else "failed", evidence=evidence))
    (output / "result.json").write_text(json.dumps(dict(schema_version=1, checks=checks)))


if __name__ == "__main__":
    evaluate(*map(Path, sys.argv[1:3]))
