import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


class ParallelTests(unittest.TestCase):
    def test_independent_crews_start_together(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            jobs = [dict(id=k, crew=k, minutes=2, needs=[]) for k in ["b", "a"]]
            (root / "plan.json").write_text(json.dumps(dict(jobs=jobs)))
            (root / "changes.jsonl").write_text("")
            (root / "windows.csv").write_text("crew,start,end\n")
            out = root / "out.json"
            subprocess.run([sys.executable, "planner.py", str(root), str(out)], check=True)
            self.assertEqual([row["start"] for row in json.loads(out.read_text())["schedule"]], [0, 0])
