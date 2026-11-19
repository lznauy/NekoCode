import json
from pathlib import Path
import sys

workspace, output = map(Path, sys.argv[1:])
fixture = Path(__file__).with_name("fixture") / "settings.ini"
expected = b"# generated v2\n" + fixture.read_bytes().replace(b"sync_window=legacy", b"sync_window=current")
target = workspace / fixture.name
ok = target.is_file() and not target.is_symlink() and target.read_bytes() == expected
script = workspace / "refresh.py"
scope = script.is_file() and not script.is_symlink() and script.read_bytes() == (fixture.parent / script.name).read_bytes()
scope = scope and {p.name for p in workspace.iterdir() if p.name != ".git"} == {fixture.name, script.name}
checks = [{"id": "edit", "status": "passed" if ok else "failed", "evidence": "Compared complete settings file"},
          {"id": "scope", "status": "passed" if scope else "failed", "evidence": "Checked workspace entries"}]
(output / "result.json").write_text(json.dumps({"schema_version": 1, "checks": checks}))
