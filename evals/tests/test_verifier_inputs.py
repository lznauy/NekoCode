import json
from pathlib import Path
import shutil
import tempfile
import unittest

from agent_eval.catalog import load_case
from agent_eval.verification import verify


class VerifierInputTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)

    def test_output_cannot_overlap_input_even_through_a_symlink(self):
        case = load_case('review-clean')
        workspace = self.root / 'workspace'
        shutil.copytree(case['paths']['fixture'], workspace)
        alias = self.root / 'alias'
        alias.symlink_to(workspace, target_is_directory=True)
        from agent_eval.storage import tree_hash
        baseline = tree_hash(workspace)
        for output in [workspace/'results', alias/'results', self.root]:
            with self.subTest(output=output), self.assertRaisesRegex(ValueError, 'overlap'):
                verify(case, workspace, output)
            self.assertEqual(tree_hash(workspace), baseline)
        self.assertFalse((workspace/'results').exists())

    def test_review_clean_non_object_answers_are_scored_failures(self):
        case = load_case('review-clean')
        for index, value in enumerate(['text', [], None, True, 7]):
            with self.subTest(value=value):
                result = verify(case, case['paths']['fixture'], self.root / str(index), json.dumps(value))
                self.assertIsNone(result['error'], result)
                self.assertFalse(result['product_passed'])
                self.assertEqual({c['id']: c['status'] for c in result['checks']},
                                 {'verdict': 'failed', 'unchanged': 'passed', 'basis': 'failed'})

    def test_grafana_invalid_enum_types_are_scored_failures(self):
        case = load_case('grafana-anonymous-review')
        workspace = self.root / 'workspace'
        (workspace / 'repo').mkdir(parents=True)
        (workspace / 'repo/go.mod').write_text('module example\n')
        for field in ['severity', 'verification']:
            for index, value in enumerate(['invalid', [], {}, None, True, 7]):
                with self.subTest(field=field, value=value):
                    finding = {'file': 'go.mod', 'start_line': 1, 'end_line': 1, 'severity': 'high',
                               'verification': 'static', 'title': 'claim', 'trigger': 'sequence',
                               'impact': 'impact', 'evidence': 'source', 'source_tasks': []}
                    finding[field] = value
                    answer = {'findings': [finding], 'reviewed_scopes': ['go.mod'], 'limitations': []}
                    result = verify(case, workspace, self.root / f'{field}-{index}', json.dumps(answer))
                    self.assertIsNone(result['error'], result)
                    self.assertEqual(result['checks'][0]['status'], 'failed')
                    self.assertFalse(result['product_passed'])

    def test_conduit_unloadable_products_are_scored_failures(self):
        case = load_case('conduit-lite')
        for index, source in enumerate(['def broken syntax\n', 'NOT_ARTICLES = []\n', 'raise SystemExit(1)\n', None]):
            with self.subTest(source=source):
                workspace = self.root / f'workspace-{index}'
                workspace.mkdir()
                if source is not None:
                    (workspace / 'app.py').write_text(source)
                result = verify(case, workspace, self.root / f'result-{index}')
                self.assertIsNone(result['error'], result)
                self.assertEqual(result['required_total'], 9)
                self.assertEqual({c['status'] for c in result['checks']}, {'failed'})
                self.assertFalse(result['product_passed'])

    def test_broken_conduit_verifier_remains_an_infrastructure_error(self):
        case = load_case('conduit-lite')
        copied = self.root / 'case'
        copied.mkdir()
        verifier = copied / 'verify.py'
        shutil.copyfile(case['paths']['verifier'], verifier)
        (copied / 'acceptance.py').write_text('def broken verifier syntax\n')
        case = {**case, 'paths': {**case['paths'], 'verifier': verifier}}
        result = verify(case, case['paths']['fixture'], self.root / 'verification')
        self.assertIsNotNone(result['error'])
        self.assertEqual({c['status'] for c in result['checks']}, {'not_run'})
        self.assertFalse(result['product_passed'])
