import functools
import http.server
import json
import os
from pathlib import Path
import subprocess
import shutil
import sys
import threading

workspace, output = Path(sys.argv[1]), Path(sys.argv[2])
requirements = ["board", "start_move", "steer", "food_growth", "pause", "wall_collision", "restart", "browser_errors", "reverse", "self_collision"]
server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), functools.partial(http.server.SimpleHTTPRequestHandler, directory=str(workspace)))
thread = threading.Thread(target=server.serve_forever, daemon=True)
thread.start()
try:
    chrome = os.environ.get("CHROME_BIN") or shutil.which("google-chrome") or shutil.which("chromium")
    if not chrome:
        raise RuntimeError("Chrome unavailable; configure CHROME_BIN")
    env = dict(os.environ, CHROME_BIN=chrome)
    proc = subprocess.run(["node", str(Path(__file__).with_name("browser.cjs")), f"http://127.0.0.1:{server.server_port}", str(output)], timeout=60, env=env)
    if proc.returncode != 0 and not (output / "result.json").exists():
        raise RuntimeError("browser verifier failed")
except (OSError, subprocess.SubprocessError, RuntimeError) as exc:
    (output / "result.json").write_text(json.dumps({"schema_version": 1, "error": str(exc), "checks": [{"id": r, "status": "not_run", "evidence": str(exc)} for r in requirements]}))
finally:
    server.shutdown()
    server.server_close()
