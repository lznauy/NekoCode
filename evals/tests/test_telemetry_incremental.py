import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from agent_eval.telemetry import Telemetry


class IncrementalTelemetryTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        (self.root/'logs').mkdir()
        self.log = self.root/'logs/llm-calls.jsonl'
        self.telemetry = Telemetry(self.root, 'session')

    def row(self, name, session='session'):
        return json.dumps({'call_id':name,'session':session,'total':10})+'\n'

    def test_unchanged_history_is_not_reparsed_and_partial_lines_are_retained(self):
        self.log.write_text(self.row('a'))
        with patch('agent_eval.telemetry.json.loads', wraps=json.loads) as parse:
            self.telemetry.refresh()
            self.assertEqual(parse.call_count,1)
            parse.reset_mock()
            self.telemetry.refresh()
            self.assertEqual(parse.call_count,0)
            row=self.row('unicode-中文')
            with self.log.open('ab') as stream:
                stream.write(row.encode()[:-2])
            self.telemetry.refresh()
            self.assertEqual(parse.call_count,0)
            with self.log.open('ab') as stream:
                stream.write(row.encode()[-2:])
            self.telemetry.refresh()
            self.assertEqual(parse.call_count,1)
        self.assertEqual(set(self.telemetry.calls),{'a','unicode-中文'})

    def test_rotation_replacement_and_copytruncate_preserve_observed_cost(self):
        self.log.write_text(self.row('a'))
        self.telemetry.refresh()
        self.log.rename(self.log.with_suffix('.jsonl.1'))
        self.log.write_text(self.row('b'))
        self.telemetry.refresh()
        self.assertEqual(set(self.telemetry.calls),{'a','b'})
        # Truncate and grow beyond the previous offset, without replacing inode.
        self.log.write_text(self.row('c')+self.row('d'))
        self.telemetry.refresh()
        self.assertEqual(set(self.telemetry.calls),{'a','b','c','d'})
        replacement=self.root/'replacement'
        replacement.write_text(self.row('e'))
        replacement.replace(self.log)
        self.telemetry.refresh()
        self.assertEqual(set(self.telemetry.calls),{'a','b','c','d','e'})

    def test_session_change_clears_scope_and_replays_only_matching_data(self):
        self.log.write_text(self.row('a')+self.row('foreign','another'))
        self.telemetry.refresh()
        self.assertEqual(set(self.telemetry.calls),{'a'})
        self.telemetry.session='another'
        self.telemetry.refresh()
        self.assertEqual(set(self.telemetry.calls),{'foreign'})
