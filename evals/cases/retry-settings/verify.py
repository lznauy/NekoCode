import json
from pathlib import Path
import sys

workspace, output = map(Path, sys.argv[1:])
name = "config/client.json"
expected = json.loads((Path(__file__).parent / "fixture" / name).read_text())
retry = {"attempts": 4, "delays_ms": [100, 300, 900]}
expected["profiles"]["production"]["retry"] = retry
files = {p.relative_to(workspace).as_posix() for p in workspace.rglob("*")
         if ".git" not in p.relative_to(workspace).parts and (p.is_file() or p.is_symlink())}
try:
    path = workspace / name
    value = json.loads(path.read_text()) if path.resolve().is_relative_to(workspace) else None
except (OSError, ValueError):
    value = None
actual = None

def check(key, ok, evidence):
    return dict(id=key, status="passed" if ok else "failed", evidence=evidence)

# Compare the other fields independently so a wrong delay doesn't hide collateral changes.
if isinstance(value, dict) and isinstance(value.get("profiles"), dict) and isinstance(value["profiles"].get("production"), dict):
    actual = value["profiles"]["production"].get("retry")
    value["profiles"]["production"]["retry"] = retry
checks = [check("active", json.dumps(actual, sort_keys=True) == json.dumps(retry, sort_keys=True), f"Production retry: {actual!r}"),
          check("preserved", files == {name} and json.dumps(value, sort_keys=True) == json.dumps(expected, sort_keys=True) and not (workspace / name).is_symlink(), "Remaining JSON values and workspace file list")]
(output / "result.json").write_text(json.dumps(dict(schema_version=1, checks=checks)))
