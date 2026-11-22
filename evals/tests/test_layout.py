import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

from agent_eval.catalog import ROOT
from agent_eval.history import collect, refresh
from agent_eval.layout import TrialLayout, new_verification
from agent_eval.storage import write_json, EVALUATION_CONTRACT_VERSION
from test_agent_eval import FAKE


class LayoutTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)

    def cli(self, *args):
        env = dict(os.environ, PYTHONPATH=str(ROOT), EVAL_TEST_TELEMETRY=str(self.root/'telemetry'))
        result = subprocess.run([sys.executable, '-m', 'agent_eval', *map(str,args)], env=env,
                                capture_output=True, text=True, timeout=30)
        self.assertEqual(result.returncode,0,result.stderr+result.stdout)
        return result

    def test_real_cli_layout_history_and_reverification(self):
        binary = self.root/'fake-headless'
        binary.write_text(FAKE.replace('#!/usr/bin/env python3','#!'+sys.executable,1)); binary.chmod(0o700)
        runs = self.root/'runs'
        self.cli('run','--case','review-clean','--binary',binary,'--model','fixed',
                 '--output',runs,'--telemetry-root',self.root/'telemetry','--allow-user-config','--label','layout-smoke')
        run = next(runs.glob('run-*'))
        manifest=json.loads((run/'run.json').read_text())
        self.assertEqual(manifest['layout_version'],2)
        self.assertEqual(manifest['source']['directory'],'source')
        self.assertFalse((run/'source/source.patch').exists())
        self.assertFalse((run/'source/untracked-source.tar.gz').exists())
        self.assertIsInstance(manifest['source']['dirty'], bool)
        self.assertTrue((run/'source/config-receipt.json').exists())
        self.assertFalse((runs/'index.md').exists())
        self.cli('history','--root',runs,'--refresh')
        self.assertTrue((runs/'index.md').exists())
        trial=run/manifest['trials'][0]
        self.assertEqual(json.loads((trial/'receipt.json').read_text())['evaluation_contract_version'], EVALUATION_CONTRACT_VERSION)
        for relative in ['trace/protocol.jsonl','trace/calls.jsonl','trace/events.jsonl',
                         'verification/v1/results/verification.json','case/fixture/cache.py','product/cache.py']:
            self.assertTrue((trial/relative).exists(),relative)
        self.assertFalse((trial/'protocol.jsonl').exists())
        self.assertTrue(json.loads((trial/'outcome.json').read_text())['success'])
        self.assertIn('layout-smoke',self.cli('history','--root',runs,'--case','review-clean').stdout)
        for relative in ['workspace', 'verification/v1/workspace', 'analysis']:
            self.assertFalse((trial/relative).exists(),relative)
        original = (trial/'product/cache.py').read_bytes()
        self.cli('reverify',trial)
        self.assertTrue((trial/'verification/v2/results/verification.json').exists())
        self.assertFalse((trial/'verification/v2/workspace').exists())
        self.assertEqual((trial/'product/cache.py').read_bytes(),original)
        self.cli('compare',run,run,'--output',self.root/'comparison')
        self.assertEqual(json.loads((self.root/'comparison/comparison.json').read_text())['counts'],{'both_success':1})

    def test_layout_version_and_append_only_outputs(self):
        trial=self.root/'trial';trial.mkdir()
        write_json(trial/'receipt.json',{'layout_version':99})
        with self.assertRaises(ValueError):TrialLayout.read(trial)
        self.assertEqual(new_verification(trial).name,'v1')
        self.assertEqual(new_verification(trial).name,'v2')
        with self.assertRaises(FileExistsError):new_verification(trial,trial/'verification/v2')
        write_json(trial/'receipt.json',{})
        with self.assertRaises(ValueError):TrialLayout.read(trial)
        for target in [trial/'product/recheck', trial/'trace/new', trial/'case/new', trial,
                       trial/'verification/v1/results/new', trial/'verification/arbitrary']:
            with self.assertRaises(ValueError):new_verification(trial,target)
            self.assertFalse(target.exists() and target != trial)
        (trial/'product').mkdir()
        (self.root/'alias').symlink_to(trial/'product',target_is_directory=True)
        with self.assertRaises(ValueError):new_verification(trial,self.root/'alias/new')

    def test_temporary_workspace_is_removed_on_interruption(self):
        from agent_eval.runner import run_trial
        from agent_eval.catalog import load_case
        observed = []
        def interrupted(*args, workspace, **options):
            workspace.mkdir()
            (workspace/'partial').write_text('partial source')
            observed.append(workspace)
            raise KeyboardInterrupt()
        with patch('agent_eval.runner._run_trial', interrupted):
            with self.assertRaises(KeyboardInterrupt):
                run_trial(load_case('review-clean'), 'unused', 'fixed', 'standard', self.root, self.root/'trial', 1)
        self.assertEqual(len(observed), 1)
        self.assertFalse(observed[0].parent.exists())

    def test_history_preserves_interrupted_and_reports_corrupt_records(self):
        run=self.root/'run-old';run.mkdir()
        write_json(run/'run.json',{'status':'interrupted','trials':['trials/pending'],'trial_specs':{'trials/pending':{'case_id':'snake','repeat':1}}})
        outside=self.root.parent/'private'
        bad=self.root/'run-bad';bad.mkdir()
        write_json(bad/'run.json',{'trials':['../../private']})
        data=refresh(self.root)
        self.assertEqual(len(data['records']),1)
        self.assertIsNone(data['records'][0]['success'])
        self.assertEqual(data['records'][0]['stop'],'unfinished')
        self.assertEqual(len(data['errors']),1)
        self.assertIn('escapes',data['errors'][0]['error'])
        self.assertEqual(data,collect(self.root))


class IndexSymlinkTests(unittest.TestCase):
    def test_refresh_does_not_follow_index_symlink(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);target=root/'original';target.write_text('untouched')
            (root/'index.md').symlink_to(target)
            with self.assertRaises(ValueError):refresh(root)
            self.assertEqual(target.read_text(),'untouched')
