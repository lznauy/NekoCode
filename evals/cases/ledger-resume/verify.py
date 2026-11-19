import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

def exercise(workspace, scenario, filename):
    sys.path.insert(0, str(workspace))
    from projector import advance
    from store import connect
    db = connect(filename)
    records = [("a", 5, 1, 7), ("a", 5, 2, -4), ("a", 6, 1, 12), ("b", 5, 1, 8)]
    with db:
        db.executemany("INSERT INTO events VALUES(?,?,?,?)", records)
    def amount(stream):
        row = db.execute("SELECT amount FROM totals WHERE stream=?", (stream,)).fetchone()
        return row[0] if row else 0
    if scenario == "ties":
        for count, total in [(1, 7), (1, 3), (1, 15), (0, 15)]:
            assert advance(db, "a", 1) == count and amount("a") == total
    elif scenario == "restart":
        assert advance(db, "a", 2) == 2
        def interrupt():
            raise InterruptedError("between apply and checkpoint")
        try:
            advance(db, "a", 3, interrupt)
        except InterruptedError:
            pass
        else:
            raise AssertionError("callback exception did not propagate")
        db.close()
        db = connect(filename)
        # A page may have committed before notification, or rolled back.
        before_retry = amount("a")
        assert before_retry in (3, 15)
        assert advance(db, "a", 3) == (1 if before_retry == 3 else 0)
        assert amount("a") == 15
        assert advance(db, "a", 3) == 0
    else:
        assert advance(db, "b", 3) == 1 and amount("b") == 8
        assert advance(db, "a", 3) == 3 and amount("a") == 15 and amount("b") == 8
    db.close()

def run(args, cwd):
    try:
        p = subprocess.run(args, cwd=cwd, capture_output=True, text=True, timeout=8, errors="replace")
        return p.returncode, (p.stdout + p.stderr)[-800:]
    except subprocess.TimeoutExpired:
        return -1, "timed out"

if __name__ == "__main__" and sys.argv[1] == "--exercise":
    exercise(Path(sys.argv[2]), sys.argv[3], sys.argv[4])
elif __name__ == "__main__":
    workspace, output = map(Path, sys.argv[1:3])
    checks = []
    with tempfile.TemporaryDirectory() as temp:
        root = Path(temp)
        for name in ["ties", "restart", "streams"]:
            code, log = run([sys.executable, "-I", __file__, "--exercise", str(workspace), name, str(root / name)], root)
            checks.append(dict(id=name, status="passed" if code == 0 else "failed", evidence=log or name))
        # Run the same delivered tests against both implementations.
        baseline = root / "baseline"
        shutil.copytree(workspace, baseline, symlinks=True, ignore=shutil.ignore_patterns("__pycache__", ".git"))
        for name in ["projector.py", "store.py"]:
            (baseline / name).unlink(missing_ok=True)
            shutil.copyfile(Path(__file__).with_name("fixture") / name, baseline / name)
        cmd = [sys.executable, "-m", "unittest", "discover"]
        fixed, fixed_log = run(cmd, workspace)
        broken, broken_log = run(cmd, baseline)
        passed = fixed == 0 and broken == 1 and "AssertionError" in broken_log
        checks.append(dict(id="diagnosis", status="passed" if passed else "failed",
                           evidence=f"Delivered tests on product: {fixed_log}; on seed: {broken_log}"))
    (output / "result.json").write_text(json.dumps(dict(schema_version=1, checks=checks)))
