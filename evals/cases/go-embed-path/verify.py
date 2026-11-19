import json
from pathlib import Path
import sys

workspace, output = map(Path, sys.argv[1:3])
try:
    answer = json.loads((output / "answer.txt").read_text())
except ValueError:
    answer = {}
# Anchor to executable source rather than release-specific line numbers.
stages = {
    "matching": (["a.txt"], "src/cmd/go/internal/load/pkg.go", ["isBadEmbedName(name) ||"]),
    "handoff": (["Files", "Patterns"], "src/cmd/go/internal/work/exec.go", ["embed.Patterns =", "embed.Files[file] ="]),
    "compile": ("build_error", "src/cmd/compile/internal/staticdata/embed.go", ["len(list) > 1", "multiple files for type"]),
    "lookup": ("fs.ErrNotExist", "src/embed/embed.go", ["Err: fs.ErrNotExist"]),
}
checks = []
for key, (expected, file, anchors) in stages.items():
    item = answer.get(key) if isinstance(answer, dict) else None
    item = item if isinstance(item, dict) else {}
    value = item.get("value")
    if isinstance(value, list) and all(isinstance(v, str) for v in value):
        value = sorted(value)
    refs = item.get("refs")
    refs = refs if isinstance(refs, list) else []
    path = workspace / "repo" / file
    lines = path.read_text().splitlines() if path.is_file() and path.resolve().is_relative_to((workspace / "repo").resolve()) else []
    cited = [lines[r["line"] - 1].strip() for r in refs if isinstance(r, dict) and
             r.get("file") == file and type(r.get("line")) is int and 1 <= r["line"] <= len(lines)]
    valid = any(anchor in line for anchor in anchors for line in cited)
    if key == "handoff":
        valid = valid or all(any(line.split() == declaration for line in cited) for declaration in
                             [["Patterns", "map[string][]string"], ["Files", "map[string]string"]])
    passed = value == expected and valid and isinstance(item.get("explanation"), str) and item["explanation"].strip()
    checks.append(dict(id=key, status="passed" if passed else "failed", evidence=f"Checked scenario result and implementation line in {file}"))
baseline = output / "input.json"
status = ("passed" if json.loads(baseline.read_text()).get("unchanged") else "failed") if baseline.exists() else "not_run"
checks.append(dict(id="unchanged", status=status, evidence="Requires pre-run workspace digest"))
(output / "result.json").write_text(json.dumps(dict(schema_version=1, checks=checks)))
