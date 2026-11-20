import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

CASE = Path(__file__).resolve().parent


def job(key, minutes, crew, *needs):
    return dict(id=key, minutes=minutes, crew=crew, needs=list(needs))


def schedule(*rows):
    return dict(schedule=[dict(id=key, start=start, end=end) for key, start, end in rows],
                finish=max((end for _, _, end in rows), default=0))


def same_json(value, expected):
    # JSON booleans and numbers must remain distinct.
    return json.dumps(value, sort_keys=True) == json.dumps(expected, sort_keys=True)


def expect_plan(path, expected):
    value = json.loads(path.read_bytes())
    if same_json(value, expected):
        return
    if isinstance(value, dict) and isinstance(value.get("schedule"), list):
        for index, (got, want) in enumerate(zip(value["schedule"], expected["schedule"])):
            if not same_json(got, want):
                raise ValueError(f"schedule[{index}] expected {want}, got {str(got)[:140]}")
        if len(value["schedule"]) != len(expected["schedule"]):
            raise ValueError(f"expected {len(expected['schedule'])} jobs, got {len(value['schedule'])}")
    raise ValueError(f"expected {expected}, got {str(value)[:140]}")


def checked_exit(result):
    if result.returncode:
        raise ValueError(f"exit {result.returncode}: {result.stderr.decode(errors='replace')[-180:]}")


def check(key, product, temp):
    bundle = temp / "shift data"
    bundle.mkdir()
    out = temp / "published plan.json"

    def cli(source):
        return subprocess.run([sys.executable, str(product / "planner.py"), str(source), str(out)],
                              cwd=temp, capture_output=True, timeout=4)

    def invoke(jobs, changes=(), windows=()):
        files = {"plan.json": json.dumps({"jobs": jobs}),
                 "changes.jsonl": "\n".join(json.dumps(dict(id=i, rev=r, minutes=m)) for i, r, m in changes),
                 "windows.csv": "crew,start,end\n" + "".join(f"{c},{a},{b}\n" for c, a, b in windows)}
        for name, content in files.items():
            (bundle / name).write_text(content)
        result = cli(bundle)
        if any((bundle / name).read_bytes() != content.encode() for name, content in files.items()):
            raise ValueError("CLI changed an input")
        return result

    def succeeds(label, expected, *args):
        out.unlink(missing_ok=True)
        try:
            checked_exit(invoke(*args))
            expect_plan(out, expected)
        except (OSError, ValueError, subprocess.TimeoutExpired) as exc:
            raise ValueError(f"{label}: {exc}") from exc
        return True

    if key == "handoff":
        expected = schedule(("audit", 3, 5), ("copy", 5, 8), ("index", 10, 13), ("release", 13, 15))
        incident = product / "incident"
        expect_plan(product / "handoff.json", expected)
        checked_exit(cli(incident))
        expect_plan(out, expected)
        for original in (CASE / "fixture/incident").iterdir():
            if (incident / original.name).read_bytes() != original.read_bytes():
                raise ValueError(f"incident input changed: {original.name}")
        return True
    if key == "dependencies":
        jobs = [job("wrap", 2, "ops", "z", "a"), job("z", 5, "ops"), job("a", 3, "db")]
        expected = schedule(("a", 0, 3), ("z", 0, 5), ("wrap", 5, 7))
        succeeds("dependencies", expected, jobs)
        succeeds("reordered dependencies", expected, jobs[::-1])
        succeeds("empty shift", schedule(), [])
        return succeeds("crew contention", schedule(("a", 0, 3), ("c", 0, 2), ("b", 3, 4)),
                        [job("b", 1, "ops"), job("c", 2, "db"), job("a", 3, "ops")])
    if key == "windows":
        jobs = [job("edge", 2, "x"), job("long", 4, "x"), job("else", 5, "y")]
        windows = [("x", 5, 8), ("x", 2, 4), ("x", 3, 5), ("x", 11, 13)]
        return succeeds("overlapping windows", schedule(("edge", 0, 2), ("else", 0, 5), ("long", 13, 17)), jobs, (), windows)
    if key == "revisions":
        jobs = [job("ship", 8, "a", "build"), job("build", 9, "b")]
        changes = [("build", 3, 2), ("ship", 1, 3), ("build", 2, 7), ("build", 3, 2)]
        expected = schedule(("build", 0, 2), ("ship", 2, 5))
        return succeeds("revision order", expected, jobs, changes) and succeeds("reversed revisions", expected, jobs, changes[::-1])
    if key == "rejection":
        valid = [job("ok", 1, "x")]
        succeeds("valid before rejection", schedule(("ok", 0, 1)), valid)
        for jobs, changes in [
            ([job("ok", 1, "x", "missing")], []),
            ([*valid, job("a", 2, "x", "b"), job("b", 1, "y", "a")], []),
            (valid, [("other", 1, 4)]),
            (valid, [("ok", 2, 5), ("ok", 1, 2), ("ok", 1, 3)]),
            ([job("ok", True, "x")], []), (valid, [("ok", 1, 0)]),
        ]:
            for existing in [True, False]:
                out.unlink(missing_ok=True)
                if existing:
                    out.write_bytes(b"archived plan\r\n")
                if invoke(jobs, changes).returncode == 0:
                    raise ValueError(f"invalid input accepted: jobs={jobs}, changes={changes}")
                if (existing and out.read_bytes() != b"archived plan\r\n") or (not existing and out.exists()):
                    raise ValueError(f"invalid input changed output (existing={existing}): {jobs}, {changes}")
        return True
    if key == "regression":
        def run(label):
            copy = temp / label
            shutil.copytree(product, copy, symlinks=True)
            if label == "prototype":
                (copy / "planner.py").unlink()
                shutil.copyfile(CASE / "fixture/planner.py", copy / "planner.py")
            if label == "serial":
                (copy / "planner.py").rename(copy / "_candidate_planner.py")
                shutil.copyfile(CASE / "serial_control.py", copy / "planner.py")
            return subprocess.run([sys.executable, "-B", "-m", "unittest", "discover", "-s", "tests"],
                                  cwd=copy, capture_output=True, timeout=10)

        checked_exit(run("candidate"))
        for label in ("prototype", "serial"):
            if run(label).returncode == 0:
                raise ValueError(f"submitted tests passed against {label} scheduling defect")
        return True
    raise ValueError(key)


def main(product, output):
    checks = []
    for req in json.loads((CASE / "case.json").read_text())["requirements"]:
        key = req["id"]
        try:
            with tempfile.TemporaryDirectory() as directory:
                passed = check(key, product.resolve(), Path(directory))
            evidence = req["description"] + ("通过" if passed else "未通过")
        except (OSError, ValueError, subprocess.TimeoutExpired) as exc:
            passed, evidence = False, str(exc)[:300]
        checks.append(dict(id=key, status="passed" if passed else "failed", evidence=evidence))
    (output / "result.json").write_text(json.dumps(dict(schema_version=1, checks=checks)))


if __name__ == "__main__":
    main(*map(Path, sys.argv[1:3]))
