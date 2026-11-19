from pathlib import Path
import os
import socket
import tempfile
import unittest
from unittest.mock import patch

from agent_eval.verification import verify


class SandboxTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name)
        self.work = self.root / "product"
        self.work.mkdir()
        directory = self.root / "case"
        directory.mkdir()
        self.script = directory / "verify.py"
        self.case = {"paths": {"verifier": self.script}, "manifest": {"id": "isolation", "tags": [],
                     "requirements": [{"id": "check", "required": True}], "budgets": {"verifier_seconds": 10}}}

    def evaluate(self):
        return verify(self.case, self.work, self.root / "out")

    def test_isolated_host_files_network_credentials_and_python_flags(self):
        sentinel = self.root / "host-only"
        sentinel.write_text("private")
        with socket.socket() as server, patch.dict(os.environ, {"PYTHONOPTIMIZE": "1", "DEPOT_PRIVATE": "value"}):
            server.bind(("127.0.0.1", 0))
            server.listen()
            self.script.write_text(f"""import json, os, socket, subprocess, sys
from pathlib import Path
w, out = map(Path, sys.argv[1:])
assert not Path({str(sentinel)!r}).exists()
assert "DEPOT_PRIVATE" not in os.environ and Path.home() == Path('/home/verify')
if sys.flags.optimize:
    raise RuntimeError('optimized verifier')
assert subprocess.check_output([sys.executable, '-c', 'import sys;print(sys.flags.optimize)']).strip() == b'0'
with socket.socket() as sock:
    assert sock.connect_ex(('127.0.0.1', {server.getsockname()[1]})) != 0
(w/'test-data').write_text('disposable')
(out/'result.json').write_text(json.dumps({{'schema_version': 1, 'checks': [
    {{'id':'check','status':'passed','evidence':'isolated'}}]}}))
""")
            result = self.evaluate()
        self.assertTrue(result["product_passed"], result)
        self.assertFalse((self.work / "test-data").exists())

    def test_missing_sandbox_is_unscored(self):
        self.script.write_text("raise RuntimeError('must not execute')")
        with patch.dict(os.environ, {"NEKOCODE_EVAL_BWRAP": "/does-not-exist/bwrap"}):
            result = self.evaluate()
        self.assertFalse(result["product_passed"])
        self.assertIn("sandbox", result["error"])
        self.assertEqual(result["checks"][0]["status"], "not_run")

    def test_sandbox_output_symlink_is_rejected(self):
        self.script.write_text("from pathlib import Path\nimport sys\n"
                               "(Path(sys.argv[2])/'result.json').symlink_to('/etc/passwd')\n")
        result = self.evaluate()
        self.assertIn("invalid verifier output", result["error"])
        self.assertFalse((self.root / "out" / "result.json").exists())
