import json
from pathlib import Path
import re
import sys

workspace, output = map(Path, sys.argv[1:])
answer = (output / "answer.txt").read_text().strip()
match = re.fullmatch(r"(?P<intro>[^\n]+)\n\s*?(?P<fence>`{3,})json[ \t]*\n(?P<body>.*?)\n(?P=fence)", answer, re.DOTALL)
example = None
if match and "毫秒" in match["intro"]:
    try:
        example = json.loads(match["body"])
    except ValueError:
        pass
correct = example == {"timeout_ms": 750} and type(example["timeout_ms"]) is int
seed = Path(__file__).with_name("fixture") / "README.md"
file = workspace / seed.name
intact = ({p.name for p in workspace.iterdir() if p.name != ".git"} == {seed.name}
          and not file.is_symlink() and file.is_file() and file.read_bytes() == seed.read_bytes())
checks = [{"id": "example", "status": "passed" if correct else "failed", "evidence": "Checked introduction and parsed fenced JSON"},
          {"id": "scope", "status": "passed" if intact else "failed", "evidence": "Compared workspace to preview fixture"}]
(output / "result.json").write_text(json.dumps({"schema_version": 1, "checks": checks}))
