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
        self.addCleanup(self.telemetry.close)

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

    def test_permission_error_is_reported_and_retried(self):
        self.log.write_text(self.row("a"))
        with patch.object(Path, "open", side_effect=PermissionError):
            self.telemetry.refresh(live=True)
        self.assertEqual(self.telemetry.read_errors, {"logs/llm-calls.jsonl": "PermissionError"})
        self.telemetry.refresh()
        self.assertEqual(set(self.telemetry.calls), {"a"})
        self.assertEqual(self.telemetry.read_errors, {})

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

    def test_poll_stops_at_the_observed_end_when_the_writer_appends(self):
        self.log.write_text(self.row('first'))
        rows = self.telemetry._rows(self.log)
        self.assertEqual(next(rows)['call_id'], 'first')
        with self.log.open('a') as writer:
            writer.write(self.row('second'))
        self.assertEqual(list(rows), [])
        self.assertEqual([r['call_id'] for r in self.telemetry._rows(self.log)], ['second'])

    def test_live_backlog_resumes_and_final_export_deduplicates_rotated_rows(self):
        journal = self.root/'sessions/session/runs/r/events.jsonl'
        journal.parent.mkdir(parents=True)
        rows = [{'run_id':'r', 'id': str(i), 'sequence':i, 'type':'step'} for i in range(4)]
        def store(items):
            journal.write_text(''.join(json.dumps(item)+'\n' for item in items))
        store(rows)
        with patch('agent_eval.telemetry.time.monotonic', side_effect=[0, 0.01, 0.11]):
            self.telemetry.refresh(live=True)
        self.assertEqual(list(self.telemetry.event_rows()), rows[:1])
        self.telemetry.refresh()
        self.assertEqual(list(self.telemetry.event_rows()), rows)
        # A rewritten journal may correct a previously observed ID. Keep its
        # original position and retain the observations removed by rotation.
        updated = {**rows[0], 'type':'tool_blocked'}
        store([updated, {'run_id':'r', 'id':'extra', 'type':'tool_started'}])
        self.telemetry.refresh()
        events = list(self.telemetry.event_rows())
        self.assertEqual(events[:-1], [updated, *rows[1:]])
        self.assertEqual(len(events), 5)
        self.assertEqual(self.telemetry.cost()['tool_blocked'], 1)
        self.telemetry.session = 'other'
        self.telemetry.refresh()
        self.assertEqual(list(self.telemetry.event_rows()), [])
