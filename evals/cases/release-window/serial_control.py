"""Verifier-only CLI control. Preserve validation and durations, serialize overlapping work."""
import json
from pathlib import Path
import subprocess
import sys

if __name__ == "__main__":
    result = subprocess.run([sys.executable, str(Path(__file__).with_name("_candidate_planner.py")), *sys.argv[1:]])
    if result.returncode:
        sys.exit(result.returncode)
    output = Path(sys.argv[2])
    plan = json.loads(output.read_bytes())
    clock = 0
    for row in plan["schedule"]:
        delay = max(0, clock - row["start"])
        row["start"] += delay
        row["end"] += delay
        clock = row["end"]
    plan["finish"] = clock
    output.write_text(json.dumps(plan))
else:
    from _candidate_planner import *
