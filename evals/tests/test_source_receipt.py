import subprocess
import tempfile
import sys
from pathlib import Path
import unittest
from unittest.mock import patch

from agent_eval.storage import source_receipt, validate_output_directory


class SourceReceiptTests(unittest.TestCase):
    def test_output_must_be_external_or_an_ignored_untracked_directory(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            repo = root / "repo"
            repo.mkdir()
            def git(*args):
                return subprocess.run(["git", "-C", str(repo), *args], check=True, capture_output=True)
            git("init", "-q")
            (repo / ".gitignore").write_text("custom-results/\nother-results/\n")
            (repo / "app.py").write_text("source\n")
            git("add", ".")
            git("-c", "user.name=Eval", "-c", "user.email=eval@localhost", "commit", "-qm", "seed")
            for name in ["custom-results", "other-results"]:
                output = repo / name
                output.mkdir()
                (output / "old-result.json").write_text("generated\n")
                validate_output_directory(repo, output)
            (repo / "new.py").write_text("new source\n")
            external = root / "external"
            external.mkdir()
            validate_output_directory(repo, external)
            unsafe = repo / "unignored-results"
            unsafe.mkdir()
            with self.assertRaisesRegex(ValueError, "ignored"):
                validate_output_directory(repo, unsafe)
            self.assertEqual(list(unsafe.iterdir()), [])
            from agent_eval.__main__ import main
            uncreated = repo / "unsafe-cli-output"
            with patch("agent_eval.__main__.ROOT", repo / "evals"), patch.object(sys, "argv", ["agent_eval", "compare", "missing", "missing", "--output", str(uncreated)]):
                with self.assertRaisesRegex(ValueError, "ignored"):
                    main()
            self.assertFalse(uncreated.exists())
            git("add", "-f", "custom-results/old-result.json")
            with self.assertRaisesRegex(ValueError, "tracked"):
                validate_output_directory(repo, repo / "custom-results")
            alias = root / "alias"
            alias.symlink_to(repo, target_is_directory=True)
            with self.assertRaisesRegex(ValueError, "ignored"):
                validate_output_directory(repo, alias / "unsafe-cli-output")

    def test_source_identity_does_not_archive_project_contents(self):
        with tempfile.TemporaryDirectory() as directory:
            repo = Path(directory)
            def git(*args):
                return subprocess.check_output(['git', '-C', str(repo), *args]).decode().strip()
            git('init', '-q')
            (repo/'app.py').write_text('source')
            git('add', '.')
            git('-c', 'user.name=Eval', '-c', 'user.email=eval@localhost', 'commit', '-qm', 'seed')
            self.assertFalse(source_receipt(repo)['dirty'])
            (repo/'.env').write_text('credential-sentinel')
            (repo/'app.py').write_text('changed')
            receipt = source_receipt(repo)
            self.assertTrue(receipt['dirty'])
            self.assertEqual(receipt['commit'], git('rev-parse', 'HEAD'))
            self.assertNotIn('credential-sentinel', str(receipt))
            self.assertEqual({p.name for p in repo.iterdir()}, {'.git', 'app.py', '.env'})
