import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

from agent_eval.catalog import ROOT, load_case
from agent_eval.runner import run_trial
from test_agent_eval import FAKE


class RepositoryCaseTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        repo = self.root / 'origin'
        repo.mkdir()
        self.git(repo, 'init', '-q')
        (repo / '.gitignore').write_text('code.go\n')
        (repo / 'go.mod').write_text('module example\n')
        (repo / 'code.go').write_text('package example\n// base\n')
        self.git(repo, 'add', '-f', '.')
        self.git(repo, '-c', 'user.name=Eval', '-c', 'user.email=eval@localhost', 'commit', '-qm', 'base')
        base = self.git(repo, 'rev-parse', 'HEAD')
        (repo / 'code.go').write_text('package example\n// review head\n')
        self.git(repo, 'add', '-f', '.')
        self.git(repo, '-c', 'user.name=Eval', '-c', 'user.email=eval@localhost', 'commit', '-qm', 'head')
        head = self.git(repo, 'rev-parse', 'HEAD')
        self.case_dir = self.root / 'cases/grafana-anonymous-review'
        self.case_dir.mkdir(parents=True)
        for name in ['prompt.md', 'verify.py', 'review.md']:
            shutil.copyfile(ROOT / 'cases/grafana-anonymous-review' / name, self.case_dir / name)
        manifest = json.loads((ROOT / 'cases/grafana-anonymous-review/case.json').read_text())
        manifest.update(version=2, fixture=None, repository={'url': repo.as_uri(), 'base': base, 'head': head})
        (self.case_dir / 'case.json').write_text(json.dumps(manifest))

    def git(self, repo, *args):
        return subprocess.check_output(['git', '-C', str(repo), *args], stderr=subprocess.PIPE).decode().strip()

    def fake_binary(self, mutate=False):
        source = FAKE.replace('#!/usr/bin/env python3', '#!' + sys.executable, 1)
        source = source.replace("answer=json.dumps({'verdict':'no_bug','reason':'The context manager closes the handle in finally.','files':['cache.py']})", "answer=json.dumps({'findings':[], 'reviewed_scopes':['code.go'], 'limitations':[]})")
        source = source.replace("if uid=='task':", "if uid=='task':\n   assert pathlib.Path('repo/code.go').is_file()\n   import subprocess\n   assert 'repo/code.go' in subprocess.check_output(['git', 'ls-files'], text=True).splitlines()\n   pathlib.Path(os.environ['EVAL_TEST_WORKSPACE']).write_text(os.getcwd())")
        if mutate:
            source = source.replace("if uid=='task':\n", "if uid=='task':\n   pathlib.Path('repo/code.go').write_text('package changed\\n')\n")
        binary = self.root / 'fake-headless'
        binary.write_text(source)
        binary.chmod(0o700)
        return binary

    def test_catalog_is_offline_and_pins_both_commits(self):
        with patch('subprocess.run', side_effect=AssertionError('catalog must not fetch')):
            case = load_case('grafana-anonymous-review', self.root)
        self.assertIsNone(case['paths']['fixture'])
        self.assertFalse((self.case_dir / 'fixture').exists())
        manifest = json.loads((self.case_dir / 'case.json').read_text())
        manifest['repository']['head'] = 'main'
        (self.case_dir / 'case.json').write_text(json.dumps(manifest))
        with self.assertRaises(ValueError):
            load_case('grafana-anonymous-review', self.root)

    def test_review_fetches_only_at_run_and_cleans_source_without_archiving_copies(self):
        case = load_case('grafana-anonymous-review', self.root)
        marker = self.root / 'workspace-location'
        with patch.dict(os.environ, EVAL_TEST_TELEMETRY=str(self.root / 'telemetry'), EVAL_TEST_WORKSPACE=str(marker)):
            outcome = run_trial(case, self.fake_binary(), 'fixed', 'standard', self.root / 'telemetry', self.root / 'trial', 1)
        self.assertTrue(outcome['success'], outcome)
        self.assertFalse(outcome['product_retained'])
        self.assertFalse(Path(marker.read_text()).exists(), 'temporary source survived trial cleanup')
        for relative in ['workspace', 'product', 'case/fixture', 'verification/v1/workspace']:
            self.assertFalse((self.root / 'trial' / relative).exists(), relative)
        receipt = json.loads((self.root / 'trial/receipt.json').read_text())
        self.assertEqual(receipt['repository']['head'], case['manifest']['repository']['head'])
        self.assertEqual(len(receipt['repository']['input_sha256']), 64)
        self.assertTrue((self.root / 'trial/trace/preparation.log').exists())
        self.assertIn('source_unchanged', (self.root / 'trial/report.md').read_text())
        self.assertFalse(list((self.root / 'trial').rglob('*.go')), 'report archive copied repository source')

    def test_fetch_failure_is_preparation_failure_and_never_starts_agent(self):
        case = load_case('grafana-anonymous-review', self.root)
        case['manifest']['repository']['url'] = (self.root / 'missing-origin').as_uri()
        marker = self.root / 'never-started'
        with patch.dict(os.environ, EVAL_TEST_TELEMETRY=str(self.root / 'telemetry'), EVAL_TEST_WORKSPACE=str(marker)):
            outcome = run_trial(case, self.fake_binary(), 'fixed', 'standard', self.root / 'telemetry', self.root / 'failed', 1)
        self.assertEqual(outcome['stop_reason'], 'preparation_failed')
        self.assertIn('repository preparation failed', outcome['error'])
        self.assertFalse(outcome['success'])
        self.assertIsNotNone(outcome['verification']['error'])
        self.assertIsNone(outcome['cost']['model_calls'])
        self.assertFalse(marker.exists())
        self.assertTrue((self.root / 'failed/outcome.json').exists())
        self.assertFalse((self.case_dir / 'fixture').exists())

    def test_review_source_mutation_fails_read_only_check_before_cleanup(self):
        case = load_case('grafana-anonymous-review', self.root)
        marker = self.root / 'mutated-workspace'
        with patch.dict(os.environ, EVAL_TEST_TELEMETRY=str(self.root / 'telemetry'), EVAL_TEST_WORKSPACE=str(marker)):
            outcome = run_trial(case, self.fake_binary(mutate=True), 'fixed', 'standard', self.root / 'telemetry', self.root / 'mutated', 1)
        self.assertFalse(outcome['success'])
        checks = {c['id']: c['status'] for c in outcome['verification']['checks']}
        self.assertEqual(checks['source_unchanged'], 'failed')
        self.assertEqual(checks['structured_report'], 'passed')
        self.assertFalse(Path(marker.read_text()).exists())
