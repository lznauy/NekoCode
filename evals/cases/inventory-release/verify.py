import csv
import json
from pathlib import Path
import re
import subprocess
import sys
import tempfile

workspace, output = map(Path, sys.argv[1:3])
checks = []
def check(name, passed, evidence):
    checks.append(dict(id=name, status="passed" if passed else "failed", evidence=evidence))

with tempfile.TemporaryDirectory() as directory:
    root = Path(directory)
    target = root / "result.csv"
    def invoke(*sources):
        try:
            return subprocess.run([sys.executable, str(workspace / "inventory.py"), "--out", str(target), *sources],
                                  cwd=root, capture_output=True, timeout=4).returncode
        except subprocess.TimeoutExpired:
            return None
    def rows():
        try:
            with target.open(encoding="utf8", newline="") as stream:
                return list(csv.reader(stream, strict=True))
        except (OSError, UnicodeError, csv.Error):
            return None
    a, b = root / "a.csv", root / "b.csv"
    a.write_text("sku,delta\nz,+5\na,-4\n")
    b.write_text("sku,delta\nz,-5\na,6\n")
    exported = rows() if invoke(a, b) == 0 else None
    check("aggregate", exported and exported[1:] == [["a", "2"], ["z", "0"]],
          "Two sources, cancellation to zero, signed quantities and output order")
    a.write_text('sku,delta\n"茶,""A""",2\n')
    b.write_text("sku,delta\n")
    quoted = rows() if invoke(b, a) == 0 else None
    empty = rows() if invoke(b) == 0 else None
    check("csv", quoted == [["sku", "total"], ['茶,"A"', "2"]] and empty == [["sku", "total"]],
          f"Expected sku,total and a quoted SKU round trip. Quoted input produced {str(quoted)[:180]}; header-only produced {str(empty)[:100]}")
    failures = []
    for sample in ["name,delta\na,2", "sku,delta\na", "sku,delta\na,2,x", "sku,delta\n,1", "sku,delta\na,1.5", None]:
        if sample is None:
            a.unlink()
        else:
            a.write_text(sample + "\n")
        target.write_bytes(b"existing\r\n")
        code = invoke(b, a)
        if code in (None, 0) or not target.is_file() or target.read_bytes() != b"existing\r\n":
            failures.append(str(sample))
    check("atomic", exported is not None and not failures, f"Cases violating rejection or preservation: {failures}")

def text(name):
    p = workspace / name
    return p.read_text(errors="replace") if p.is_file() and not p.is_symlink() else ""
report = text("STATUS.md")
check("handover", bool(re.search(r"python3?\s+inventory\.py\s+--out\s+\S+\s+\S+", text("README.md"))) and
      all(any(key in line.lower() and "PASS" in line and len(line.strip()) > len(key)+6 for line in report.splitlines())
          for key in ("csv", "aggregate", "atomic")) and all(c["status"] == "passed" for c in checks),
      "README CLI invocation and status entries checked against independently executed release gates")
(output / "result.json").write_text(json.dumps(dict(schema_version=1, checks=checks)))
