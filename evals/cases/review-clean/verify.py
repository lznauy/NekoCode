import json
from pathlib import Path
import sys

workspace, output = Path(sys.argv[1]), Path(sys.argv[2])
original = Path(__file__).with_name("fixture") / "cache.py"
answer = (output / "answer.txt").read_text()
try:
    value = json.loads(answer)
except ValueError:
    value = {}
if not isinstance(value, dict):
    value = {}
actual_files = {p.relative_to(workspace).as_posix() for p in workspace.rglob("*")
                if (p.is_file() or p.is_symlink()) and not any(part in {".git", "__pycache__"} for part in p.relative_to(workspace).parts)}
unchanged = actual_files == {"cache.py"} and not (workspace / "cache.py").is_symlink() and (workspace / "cache.py").read_bytes() == original.read_bytes()
checks = [
    {"id": "verdict", "status": "passed" if value.get("verdict") == "no_bug" else "failed", "evidence": "final verdict must reject the false leak claim"},
    {"id": "unchanged", "status": "passed" if unchanged else "failed", "evidence": "cache.py bytes compared to frozen seed"},
    {"id": "basis", "status": "passed" if isinstance(value.get("reason"), str) and len(value["reason"].strip()) >= 10 and isinstance(value.get("files"), list) and "cache.py" in value["files"] else "failed", "evidence": "structured explanation and file reference present; causal correctness needs human review"},
]
(output / "result.json").write_text(json.dumps({"schema_version": 1, "checks": checks}))
