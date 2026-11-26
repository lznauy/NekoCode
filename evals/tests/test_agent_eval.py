import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import time
import signal
import unittest
from unittest.mock import patch

from agent_eval.catalog import ROOT, load_case, load_suite
from agent_eval.compare import compare
from agent_eval.runner import run_trial, model_observation
from agent_eval.storage import read_json, tree_hash, write_json
from agent_eval.telemetry import Telemetry
from agent_eval.verification import verify, stop_process


FAKE = r'''#!/usr/bin/env python3
import json,os,pathlib,sys
root=pathlib.Path(os.environ['EVAL_TEST_TELEMETRY'])
sid='test-session'
polls=0
def emit(x):
 print(json.dumps(x),flush=True)
def response(f,body):
 emit({'type':'control_response','response':{'subtype':'success','request_id':f['request_id'],'response':body}})
for line in sys.stdin:
 f=json.loads(line)
 if f['type']=='user':
  uid=f['uuid'];emit({'type':'system','subtype':'init','session_id':sid})
  if uid=='setup-mode':
   mode=f['message']['content'].split()[-1]
   emit({'type':'system','subtype':'message','message':'Mode: '+mode})
  if uid=='task':
   d=root/'sessions'/sid/'runs'/'run-one';d.mkdir(parents=True,exist_ok=True)
   answer=json.dumps({'verdict':'no_bug','reason':'The context manager closes the handle in finally.','files':['cache.py']})
   es=[{'id':'start','run_id':'run-one','type':'run_started','source':{'kind':'headless','id':'task'}},{'id':'tool-1','run_id':'run-one','type':'tool_started','payload':{'tool_name':'read','input':{'path':'cache.py'}}},{'id':'done-1','run_id':'run-one','type':'run_done','payload':{'output':answer}}]
   if os.environ.get('EVAL_TEST_BEHAVIOR')=='different-log-answer':es[-1]['payload']['output']='private log is not the delivered answer'
   if os.environ.get('EVAL_TEST_BEHAVIOR')=='coordinator-failed':es += [{'id':'cs','run_id':'coord','type':'run_started','source':{'kind':'task_completion'}},{'id':'cf','run_id':'coord','type':'run_failed','payload':{}}]
   if mode=='super-agent' and os.environ.get('EVAL_TEST_BEHAVIOR')!='missing-background':(root/'sessions'/sid/'background.json').write_text(json.dumps({'version':2,'tasks':[]}))
   (d/'events.jsonl').write_text(''.join(json.dumps(e)+'\n' for e in es))
   logs=root/'logs';logs.mkdir(parents=True,exist_ok=True)
   call={'session':sid,'call_id':'call-one','source':'main','model':'' if os.environ.get('EVAL_TEST_BEHAVIOR')=='missing-model' else 'fixed','in':100,'out':20,'total':120,'new':80,'cached':20,'cache_usage_reported':True,'execution':{'run_id':'run-one'},'seq':1}
   (logs/'llm-calls.jsonl').write_text(json.dumps(call)+'\n')
  if uid=='task' and os.environ.get('EVAL_TEST_BEHAVIOR')=='step-limit':
   emit({'type':'result','user_message_uuid':uid,'is_error':True,'stop_reason':'step_limit','subtype':'error_step_limit','result':'Outstanding verification'})
   continue
  if uid=='task' and os.environ.get('EVAL_TEST_BEHAVIOR')=='incomplete':
   emit({'type':'result','user_message_uuid':uid,'is_error':True,'stop_reason':'incomplete','subtype':'error_incomplete','result':'incomplete'})
   continue
  if uid=='task' and os.environ.get('EVAL_TEST_BEHAVIOR')=='budget-result':
   emit({'type':'result','user_message_uuid':uid,'is_error':True,'stop_reason':'call_budget','subtype':'error_call_budget','execution_budget':{'limit':400,'admitted':398,'work_remaining':0,'handoff_remaining':2,'coordinator_remaining':0}})
   continue
  if uid!='task' or os.environ.get('EVAL_TEST_BEHAVIOR')!='undelivered-answer':emit({'type':'result','user_message_uuid':uid,'result':answer if uid=='task' else '', 'is_error':False})
  if uid=='task' and os.environ.get('EVAL_TEST_BEHAVIOR')=='public-coordinator':
   emit({'type':'system','subtype':'init','user_message_uuid':'coordinator-run','nekocode_source':{'kind':'task_completion'}})
   emit({'type':'result','user_message_uuid':'coordinator-run','result':answer.replace('finally.', 'finally; coordinator confirmed.'),'is_error':False})
 elif f['type']=='control_request':
  subtype=f['request']['subtype']
  if subtype=='shutdown':response(f,{});break
  if subtype=='model.set':response(f,{'model':'fixed'})
  elif subtype=='server.info':response(f,{'model':{'model':'fixed'},'tools':['read'], **({'execution_budget':{'limit':1,'admitted':0,'work_remaining':0,'handoff_remaining':0,'coordinator_remaining':1}} if os.environ.get('EVAL_TEST_BEHAVIOR','').startswith('native-budget') else {})})
  elif subtype=='extensions.list':response(f,{})
  elif subtype=='context.snapshot':
   polls+=1
   response(f,{'session_id':sid,'context':{}, **({'run_id':'run-one'} if os.environ.get('EVAL_TEST_BEHAVIOR')=='native-budget-slow-tool' and polls<=4 else {})})
'''


class EvalTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)

    def test_model_identity_allows_avoiding_delegation(self):
        main={'source':'main','model':'fixed','effective_effort':'low'}
        worker={**main,'source':'subagent'}
        compact={**main,'source':'compaction','model':'compact'}
        base,aux=model_observation({'model':'fixed'},[main,worker,compact])
        candidate,_=model_observation({'model':'fixed'},[main])
        self.assertEqual(base,candidate)
        self.assertEqual(aux,{'compaction':[('compact','','low')]})
        different,_=model_observation({'model':'fixed'},[main,{**worker,'model':'different'}])
        self.assertNotEqual(base,different)

    def test_catalog_resources_and_suite(self):
        suite, cases = load_suite('smoke')
        self.assertEqual(len(cases), 3)
        with self.assertRaises(ValueError): load_case('../snake')
        copied = self.root/'catalog'
        shutil.copytree(ROOT/'cases', copied/'cases')
        first = load_case('snake', copied)
        helper = copied/'cases/snake/browser.cjs'
        helper.write_text(helper.read_text()+'\n// verifier changed\n')
        self.assertNotEqual(first['fingerprint'], load_case('snake', copied)['fingerprint'])

    def test_conduit_seed_fails_and_known_product_passes(self):
        case = load_case('conduit-lite')
        bad = verify(case, case['paths']['fixture'], self.root/'seed')
        self.assertFalse(bad['product_passed'])
        self.assertGreater(bad['required_total']-bad['required_passed'], 0)
        reference = ROOT/'tests/oracles/conduit-lite'
        shutil.copytree(reference, self.root/'reference')
        good = verify(case, self.root/'reference', self.root/'fixed')
        self.assertTrue(good['product_passed'], good)

    def test_negative_review_rejects_false_positive_and_mutation(self):
        case = load_case('review-clean')
        shutil.copytree(case['paths']['fixture'], self.root/'workspace')
        answer=json.dumps({'verdict':'no_bug','reason':'The handle is closed in the context manager finally.','files':['cache.py']})
        self.assertTrue(verify(case,self.root/'workspace',self.root/'good',answer)['product_passed'])
        (self.root/'workspace/unrequested.txt').write_text('unnecessary report')
        self.assertFalse(verify(case,self.root/'workspace',self.root/'extra',answer)['product_passed'])
        (self.root/'workspace/unrequested.txt').unlink()
        wrong=answer.replace('no_bug','bug')
        self.assertFalse(verify(case,self.root/'workspace',self.root/'wrong',wrong)['product_passed'])
        (self.root/'workspace/cache.py').write_text('pass\n')
        self.assertFalse(verify(case,self.root/'workspace',self.root/'modified',answer)['product_passed'])

    def test_telemetry_rotation_dedup_and_missing_is_unknown(self):
        telemetry=Telemetry(self.root,'session')
        self.assertIsNone(telemetry.cost()['model_calls'])
        logs=self.root/'logs';logs.mkdir()
        row={'call_id':'a','session':'session','total':12,'in':10,'out':2,'new':8,'cached':2,'cache_usage_reported':True}
        for name in ['llm-calls.jsonl','llm-calls.jsonl.1']:(logs/name).write_text(json.dumps(row)+'\n'+json.dumps({'call_id':'other','session':'another'})+'\n')
        telemetry.refresh()
        self.assertEqual(telemetry.cost()['model_calls'],1)
        self.assertEqual(telemetry.cost()['fresh_tokens'],8)
        row['call_id']='b';row.pop('in');row['cache_usage_reported']=False
        (logs/'llm-calls.jsonl').write_text(json.dumps(row)+'\n')
        telemetry.refresh()
        self.assertIsNone(telemetry.cost()['input_tokens'])
        self.assertIsNone(telemetry.cost()['fresh_tokens'])

    def fake_binary(self):
        binary=self.root/'fake-headless'
        binary.write_text(FAKE.replace('#!/usr/bin/env python3', '#!'+sys.executable,1))
        binary.chmod(0o700)
        return binary

    def test_real_protocol_driver_with_fake_backend_and_product_snapshot(self):
        case=load_case('review-clean')
        with patch.dict(os.environ,{'EVAL_TEST_TELEMETRY':str(self.root/'telemetry')}):
            outcome=run_trial(case,self.fake_binary(),'fixed','standard',self.root/'telemetry',self.root/'trial',1,config_fingerprint='a'*64)
        self.assertTrue(outcome['success'],outcome)
        self.assertEqual(outcome['cost']['model_calls'],1)
        self.assertEqual(outcome['cost']['total_tokens'],120)
        self.assertEqual(tree_hash(self.root/'trial/product'),tree_hash(case['paths']['fixture']))
        frames = [json.loads(line)["message"] for line in (self.root/'trial/trace/protocol.jsonl').read_text().splitlines()]
        initialize = next(f["request"] for f in frames if f.get("request_id") == "initialize")
        self.assertEqual(initialize["external_token_limit"], case["manifest"]["budgets"]["total_tokens"])
        self.assertFalse((self.root/'trial/analysis').exists())
        self.assertFalse((self.root/'trial/workspace').exists())
        with self.assertRaises(FileExistsError):
            run_trial(case,self.fake_binary(),'fixed','standard',self.root/'telemetry',self.root/'trial',1)

    def test_unreadable_journal_preserves_trial_outcome(self):
        telemetry = self.root / "telemetry"
        trial = self.root / "trial"
        journal = Path("sessions/test-session/runs/unreadable/events.jsonl")
        (telemetry / journal).mkdir(parents=True)
        with patch.dict(os.environ, {"EVAL_TEST_TELEMETRY": str(telemetry)}):
            run_trial(load_case("review-clean"), self.fake_binary(), "fixed", "standard", telemetry, trial, 1)
        outcome = json.loads((trial / "outcome.json").read_text())
        self.assertEqual(outcome["integrity"]["telemetry_read_errors"], {str(journal): "NotRegularFile"})
        self.assertEqual(outcome["cost"]["tool_started"], 1)
        self.assertEqual(json.loads((trial / "state.json").read_text())["status"], "finished")

    def test_private_log_cannot_replace_public_answer(self):
        trial = self.root / 'public-answer'
        with patch.dict(os.environ, {'EVAL_TEST_TELEMETRY': str(self.root/'telemetry'), 'EVAL_TEST_BEHAVIOR': 'different-log-answer'}):
            outcome = run_trial(load_case('review-clean'), self.fake_binary(), 'fixed', 'standard', self.root/'telemetry', trial, 1)
        self.assertTrue(outcome['success'], outcome)
        self.assertIn('finally.', (trial/'answer.txt').read_text())
        self.assertNotIn('private log', (trial/'answer.txt').read_text())

    def test_public_coordinator_result_becomes_final_answer(self):
        trial = self.root / 'coordinator-answer'
        with patch.dict(os.environ, {'EVAL_TEST_TELEMETRY': str(self.root/'telemetry'), 'EVAL_TEST_BEHAVIOR': 'public-coordinator'}):
            outcome = run_trial(load_case('review-clean'), self.fake_binary(), 'fixed', 'super-agent', self.root/'telemetry', trial, 1)
        self.assertTrue(outcome['success'], outcome)
        self.assertIn('coordinator confirmed', (trial/'answer.txt').read_text())

    def test_invalid_protocol_retains_failure_record(self):
        binary=self.root/'bad-headless';binary.write_text('#!'+sys.executable+'\nprint("not json",flush=True)\n');binary.chmod(0o700)
        outcome=run_trial(load_case('review-clean'),binary,'fixed','standard',self.root/'telemetry',self.root/'bad',1)
        self.assertFalse(outcome['success'])
        self.assertEqual(outcome['stop_reason'],'harness_error')
        self.assertIsNone(outcome['cost']['model_calls'])
        self.assertTrue((self.root/'bad/outcome.json').exists())

    def test_missing_actual_model_has_no_observed_fingerprint(self):
        with patch.dict(os.environ,{'EVAL_TEST_TELEMETRY':str(self.root/'telemetry'),'EVAL_TEST_BEHAVIOR':'missing-model'}):
            result=run_trial(load_case('review-clean'),self.fake_binary(),'fixed','standard',self.root/'telemetry',self.root/'unknown-model',1)
        self.assertIsNone(read_json(self.root/'unknown-model/receipt.json')['observed_model_fingerprint'])

    def test_super_missing_state_cannot_settle(self):
        case=load_case('review-clean')
        case['manifest']['budgets']['wall_seconds']=5
        with patch.dict(os.environ,{'EVAL_TEST_TELEMETRY':str(self.root/'telemetry'),'EVAL_TEST_BEHAVIOR':'missing-background'}):
            result=run_trial(case,self.fake_binary(),'fixed','super-agent',self.root/'telemetry',self.root/'missing',1)
        self.assertEqual(result['stop_reason'],'wall_budget')
        self.assertFalse(result['success'])
        self.assertTrue(result['verification']['product_passed'])
        self.assertFalse(result['integrity']['background_state_observed'])

    def test_coordinator_failure_is_not_delivery_success(self):
        with patch.dict(os.environ,{'EVAL_TEST_TELEMETRY':str(self.root/'telemetry'),'EVAL_TEST_BEHAVIOR':'coordinator-failed'}):
            result=run_trial(load_case('review-clean'),self.fake_binary(),'fixed','super-agent',self.root/'telemetry',self.root/'failure',1)
        self.assertEqual(result['stop_reason'],'harness_error')
        self.assertFalse(result['success'])
        self.assertEqual(result['failed_runtime_runs'],['coord'])

    def test_call_budget_does_not_invent_undelivered_answer(self):
        case=load_case('review-clean');case['manifest']['budgets']['model_calls']=1
        with patch.dict(os.environ,{'EVAL_TEST_TELEMETRY':str(self.root/'telemetry'),'EVAL_TEST_BEHAVIOR':'undelivered-answer'}):
            result=run_trial(case,self.fake_binary(),'fixed','standard',self.root/'telemetry',self.root/'budget',1)
        self.assertEqual(result['stop_reason'],'call_budget')
        self.assertFalse(result['success'])
        self.assertFalse(result['verification']['product_passed'])
        self.assertFalse(result['public_final_answer_present'])
        self.assertEqual((self.root/'budget/answer.txt').read_text(), '')

    def test_unbudgeted_run_observes_cost_without_enforcing_case_limits(self):
        case = load_case('review-clean')
        case['manifest']['budgets'].update(model_calls=1, total_tokens=1, wall_seconds=1)
        path = self.root/'unbudgeted'
        with patch.dict(os.environ, {'EVAL_TEST_TELEMETRY': str(self.root/'telemetry')}):
            result = run_trial(case, self.fake_binary(), 'fixed', 'standard', self.root/'telemetry', path, 1, no_execution_budget=True)
        self.assertTrue(result['success'], result)
        self.assertEqual(result['cost']['total_tokens'], 120)
        receipt = json.loads((path/'receipt.json').read_text())
        for key in ('model_calls', 'total_tokens', 'wall_seconds'):
            self.assertIsNone(receipt['budgets'][key])
        frames = [json.loads(line)['message'] for line in (path/'trace/protocol.jsonl').read_text().splitlines()]
        init = next(f['request'] for f in frames if f.get('request_id') == 'initialize')
        self.assertEqual(init, {'subtype': 'initialize'})

    def test_step_limit_preserves_partial_delivery(self):
        path = self.root/'step-limit'
        with patch.dict(os.environ, {'EVAL_TEST_TELEMETRY': str(self.root/'telemetry'), 'EVAL_TEST_BEHAVIOR': 'step-limit'}):
            result = run_trial(load_case('review-clean'), self.fake_binary(), 'fixed', 'standard', self.root/'telemetry', path, 1)
        self.assertEqual(result['stop_reason'], 'step_limit')
        self.assertFalse(result['success'])
        self.assertEqual((path/'answer.txt').read_text().strip(), 'Outstanding verification')

    def test_incomplete_model_result_preserves_stop_reason(self):
        telemetry = self.root/'telemetry'
        with patch.dict(os.environ, {'EVAL_TEST_TELEMETRY': str(telemetry), 'EVAL_TEST_BEHAVIOR': 'incomplete'}):
            result = run_trial(load_case('review-clean'), self.fake_binary(), 'fixed', 'standard', telemetry, self.root/'trial', 1)
        self.assertEqual(result['stop_reason'], 'incomplete')
        self.assertFalse(result['success'])

    def test_budget_result_preserves_role_balances(self):
        path = self.root/'budget-result'
        with patch.dict(os.environ, {'EVAL_TEST_TELEMETRY':str(self.root/'telemetry'), 'EVAL_TEST_BEHAVIOR':'budget-result'}):
            result = run_trial(load_case('review-clean'),self.fake_binary(),'fixed','standard',self.root/'telemetry',path,1)
        self.assertEqual(result['stop_reason'], 'call_budget')
        self.assertFalse(result['success'])
        self.assertIn('398/400 admitted; remaining ordinary 0, worker handoff 2, coordinator 0', (path/'report.md').read_text())

    def test_native_budget_exact_ceiling_can_settle(self):
        case=load_case('review-clean');case['manifest']['budgets']['model_calls']=1
        with patch.dict(os.environ,{'EVAL_TEST_TELEMETRY':str(self.root/'telemetry'),'EVAL_TEST_BEHAVIOR':'native-budget'}):
            result=run_trial(case,self.fake_binary(),'fixed','standard',self.root/'telemetry',self.root/'native-budget',1)
        self.assertEqual(result['stop_reason'],'settled')
        self.assertTrue(result['success'])
        self.assertEqual(result['execution_budget']['limit'],1)

    def test_native_budget_does_not_add_an_implicit_settlement_deadline(self):
        case = load_case('review-clean')
        case['manifest']['budgets'].update(model_calls=1, wall_seconds=1000)
        ticks = iter(range(10000))
        with patch.dict(os.environ, {'EVAL_TEST_TELEMETRY': str(self.root/'telemetry'),
                                     'EVAL_TEST_BEHAVIOR': 'native-budget-slow-tool'}), \
                patch('agent_eval.runner.time') as clock:
            # Advance logical time while the accepted run finishes its tools.
            # No real 30-second sleep is needed to exercise the former cutoff.
            clock.monotonic.side_effect = lambda: next(ticks)
            result = run_trial(case, self.fake_binary(), 'fixed', 'standard',
                               self.root/'telemetry', self.root/'slow-native-budget', 1)
        self.assertGreater(result['elapsed_s'], 30)
        self.assertEqual(result['stop_reason'], 'settled')
        self.assertTrue(result['success'])

    @unittest.skipUnless(os.name=='posix','process-group lifecycle')
    def test_cleanup_kills_descendant_after_leader_exits(self):
        pidfile=self.root/'child.pid'
        child="import signal,time;signal.signal(signal.SIGTERM,signal.SIG_IGN);time.sleep(60)"
        script="import subprocess,sys,pathlib; p=subprocess.Popen([sys.executable,'-c',"+repr(child)+"]);pathlib.Path("+repr(str(pidfile))+").write_text(str(p.pid))"
        parent=subprocess.Popen([sys.executable,'-c',script],start_new_session=True)
        try:
            parent.wait(timeout=3)
            childpid=int(pidfile.read_text())
        finally:
            stop_process(parent)
        for _ in range(30):
            status=Path('/proc')/str(childpid)/'stat'
            if not status.exists() or status.read_text().split()[2]=='Z':break
            time.sleep(.01)
        else:self.fail('descendant remained alive after group cleanup')

    @unittest.skipUnless(os.environ.get('NEKOCODE_EVAL_BROWSER_TESTS')=='1','optional real Chrome verifier self-check')
    def test_snake_verifier_positive_and_mutations(self):
        case=load_case('snake')
        reference=ROOT/'tests/oracles/snake'
        self.assertTrue(verify(case,reference,self.root/'snake-good')['product_passed'])
        for name,old,new,required in [
            ('collision','head.x<0||head.x>=12||head.y<0||head.y>=12','false','wall_collision'),
            ('timer','clearInterval(timer);snake=','snake=','restart'),
            ('growth','if(eat){score++;spawn();}else snake.pop();','if(eat){spawn();snake.pop();}else snake.pop();','food_growth')]:
            folder=self.root/('snake-'+name);shutil.copytree(reference,folder)
            file=folder/'index.html';text=file.read_text();self.assertIn(old,text);text=text.replace(old,new)
            if name=='timer':text=text.replace("status='running';clearInterval(timer);timer=setInterval","status='running';timer=setInterval")
            file.write_text(text)
            result=verify(case,folder,self.root/('check-'+name))
            checks={c['id']:c['status'] for c in result['checks']}
            self.assertFalse(result['product_passed'],(name,result))
            self.assertEqual(checks[required],'failed',(name,result))

    def make_run(self,name,success=True,fingerprint='case',model='model',tokens=100):
        directory=self.root/name
        trial=directory/'trials/one';trial.mkdir(parents=True)
        write_json(directory/'run.json',{'run_id':name,'trials':['trials/one']})
        write_json(trial/'receipt.json',{'case_id':'snake','repeat':1,'case_fingerprint':fingerprint,'model_requested':model,'mode':'standard','interaction_policy':'deny','external_config_fingerprint':'a'*64,'environment':{},'telemetry_adapter':'test','evaluation_contract_version':3,'evaluator_fingerprint':'test','observed_model_fingerprint':'fixed-model','budgets':{'calls':10}})
        write_json(trial/'outcome.json',{'success':success,'stop_reason':'settled' if success else 'wall_budget','verification':{'error':None,'required_passed':8 if success else 4},'cost':{'total_tokens':tokens,'model_calls':4,'input_tokens':tokens-10,'output_tokens':10,'fresh_tokens':None,'cached_tokens':None,'tool_calls':3},'elapsed_s':20,'integrity':{'calls_observed':True,'all_call_ids_present':True}})
        return directory

    def test_comparison_quality_before_cost_and_case_hash_matching(self):
        base=self.make_run('base')
        candidate=self.make_run('candidate',tokens=80)
        c=compare(base,candidate,self.root/'comparison')
        self.assertEqual(c['counts'],{'both_success':1})
        self.assertEqual(c['successful_pair_cost']['total_tokens']['median_candidate_minus_baseline'],-20)
        self.assertEqual(c['successful_pair_cost']['input_tokens']['median_candidate_minus_baseline'],-20)
        self.assertEqual(c['successful_pair_cost']['fresh_tokens']['paired_success_samples'],0)
        self.assertEqual(c['successful_pair_cost']['tool_requests']['paired_success_samples'],0)
        for run, requests in [(base,5),(candidate,3)]:
            path=run/'trials/one/outcome.json'
            outcome=read_json(path);outcome['cost']['tool_requests']=requests;write_json(path,outcome)
        observed=compare(base,candidate,self.root/'comparison-tool-requests')
        self.assertEqual(observed['successful_pair_cost']['tool_requests']['median_candidate_minus_baseline'],-2)
        regression=self.make_run('regression',success=False,tokens=10)
        c=compare(base,regression,self.root/'regression-comparison')
        self.assertEqual(c['counts'],{'regression':1})
        self.assertEqual(c['successful_pair_cost']['total_tokens']['paired_success_samples'],0)
        changed=self.make_run('changed',fingerprint='changed')
        self.assertEqual(compare(base,changed,self.root/'changed-comparison')['counts'],{'incomparable':1})
        for field in ['observed_model_fingerprint','telemetry_adapter']:
            other=self.make_run('different-'+field)
            receipt=read_json(other/'trials/one/receipt.json');receipt[field]='different';write_json(other/'trials/one/receipt.json',receipt)
            self.assertEqual(compare(base,other,self.root/('compare-'+field))['counts'],{'incomparable':1})
        incomplete=self.make_run('incomplete');(incomplete/'trials/one/outcome.json').unlink()
        self.assertEqual(compare(base,incomplete,self.root/'compare-incomplete')['counts'],{'incomparable':1})

    def test_comparison_treats_legacy_parallel_step_sharing_as_disabled(self):
        base = self.make_run("baseline")
        candidate = self.make_run("candidate")
        path = candidate / "trials/one/receipt.json"
        receipt = read_json(path)
        for enabled, expected in [(False, "both_success"), (True, "incomparable")]:
            receipt["parallel_step_sharing"] = enabled
            write_json(path, receipt)
            result = compare(base, candidate, self.root / f"compare-{enabled}")
            self.assertEqual(result["counts"], {expected: 1})

    def test_comparison_excludes_only_metrics_from_unreadable_logs(self):
        base = self.make_run('baseline')
        candidate = self.make_run('candidate', tokens=60)
        path = candidate / 'trials/one/outcome.json'
        outcome = read_json(path)
        for name, log, unknown, known in [
            ('calls', 'logs/llm-calls.jsonl.1', 'total_tokens', 'tool_calls'),
            ('events', 'sessions/s/runs/r/events.jsonl', 'tool_calls', 'total_tokens'),
        ]:
            with self.subTest(source=name):
                outcome['integrity']['telemetry_read_errors'] = {log: 'PermissionError'}
                write_json(path, outcome)
                result = compare(base, candidate, self.root / name)
                self.assertEqual(result['counts'], {'both_success': 1})
                reported = result['pairs'][0]['candidate']['cost']
                self.assertIsNone(reported[unknown])
                self.assertEqual(reported[known], outcome['cost'][known])
                costs = result['successful_pair_cost']
                self.assertEqual(costs[unknown]['paired_success_samples'], 0)
                self.assertEqual(costs[known]['paired_success_samples'], 1)
                self.assertEqual(costs['elapsed_s']['paired_success_samples'], 1)
                delivery = result['cost_per_delivery']['snake']['candidate']
                self.assertEqual(delivery['total_tokens_per_delivery'], None if name == 'calls' else 60)

    def test_delivery_cost_counts_failed_attempts_without_imputing_unknown_cost(self):
        from agent_eval.compare import delivery_cost
        ok = read_json(self.make_run("good") / "trials/one/outcome.json")
        fail = read_json(self.make_run("failure", success=False, tokens=60) / "trials/one/outcome.json")
        self.assertEqual(delivery_cost([ok, fail]), {"attempts": 2, "deliveries": 1,
                         "model_calls_per_delivery": 8, "total_tokens_per_delivery": 160})
        self.assertIsNone(delivery_cost([fail])["model_calls_per_delivery"])
        fail["cost"]["total_tokens"] = None
        self.assertIsNone(delivery_cost([ok, fail])["total_tokens_per_delivery"])
        fail["integrity"]["all_call_ids_present"] = False
        self.assertIsNone(delivery_cost([ok, fail])["model_calls_per_delivery"])

    def test_comparison_uses_contract_not_presentation_source_identity(self):
        base = self.make_run('contract-base')
        candidate = self.make_run('contract-candidate')
        for run, fingerprint in [(base, 'old-source'), (candidate, 'new-source')]:
            path = run / 'trials/one/receipt.json'
            receipt = read_json(path)
            receipt['evaluation_contract_version'] = 1
            receipt['evaluator_fingerprint'] = fingerprint
            write_json(path, receipt)
        self.assertEqual(compare(base, candidate, self.root / 'same-contract')['counts'], {'both_success': 1})
        path = candidate / 'trials/one/receipt.json'
        receipt['evaluation_contract_version'] = 2
        write_json(path, receipt)
        self.assertEqual(compare(base, candidate, self.root / 'changed-contract')['counts'], {'incomparable': 1})
        del receipt['evaluation_contract_version']
        write_json(path, receipt)
        self.assertEqual(compare(base, candidate, self.root / 'legacy-contract')['counts'], {'incomparable': 1})

    def test_verifier_missing_dependency_is_not_product_success(self):
        case=load_case('snake')
        # Empty product is never green, whether the browser is available or not.
        with patch.dict(os.environ, {'NODE_PATH':'', 'CHROME_BIN':'/missing-chrome'}):
            result=verify(case,case['paths']['fixture'],self.root/'browser-missing')
        self.assertFalse(result['product_passed'])




class ToolCostTests(unittest.TestCase):
    def test_image_rechecks_follow_content_across_prompts_and_paths(self):
        from agent_eval.telemetry import protocol_tool_cost
        rows = []
        for index, digest in enumerate(["a"*64, "a"*64, "b"*64]):
            call = str(index)
            rows.append(dict(type="control_request", request=dict(tool_name="image_understand",
                             tool_use_id=call, input={"_nekocode_approved_image_sha256": digest})))
            rows.append(dict(type="assistant", session_id="s", user_message_uuid="task",
                        message=dict(content=[dict(type="tool_use", id=call, name="image_understand",
                                                   input=dict(path=call, prompt=call))])))
            rows.append(dict(type="user", session_id="s", user_message_uuid="task",
                        message=dict(content=[dict(type="tool_result", tool_use_id=call,
                                                   is_error=index == 0, content="API error (HTTP 429)" if index == 0 else "ok")])))
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / "protocol.jsonl"
            for frames, expected in [(rows, 1), (rows[1:], None)]:
                path.write_text("".join(json.dumps(dict(direction="agent", message=f)) + "\n" for f in frames))
                cost = protocol_tool_cost(path, "s")
                self.assertEqual(cost["same_image_rechecks"], expected)
                self.assertEqual(cost["image_rate_limits"], 1)

    def test_protocol_requests_deduplicate_and_exclude_setup_children(self):
        from agent_eval.telemetry import protocol_tool_cost
        with tempfile.TemporaryDirectory() as d:
            path = Path(d) / 'protocol.jsonl'
            def frame(kind, blocks, **kw):
                return {'direction': 'agent', 'message': dict(type=kind, session_id='s', run_id='r',
                        user_message_uuid='task', message={'content': blocks}, **kw)}
            request = frame('assistant', [{'type': 'tool_use', 'id': 'a'}, {'type': 'tool_use', 'id': 'b'}])
            error = frame('user', [{'type': 'tool_result', 'tool_use_id': 'a', 'is_error': True}])
            child = frame('assistant', [{'type': 'tool_use', 'id': 'child'}], parent_tool_use_id='p')
            setup = frame('assistant', [{'type': 'tool_use', 'id': 'setup'}])
            setup['message']['user_message_uuid'] = 'setup-mode'
            other = frame('assistant', [{'type': 'tool_use', 'id': 'other'}])
            other['message']['session_id'] = 's/child'
            real_child = frame('assistant', [{'type': 'tool_use', 'id': 'real-child'}], parent_tool_use_id=None, nekocode_subagent_id='worker')
            child_error = frame('user', [{'type': 'tool_result', 'tool_use_id': 'real-child', 'is_error': True}], nekocode_subagent_id='worker')
            notice = frame("system", [])
            notice["message"].update(subtype="message", message="Goal awaits your review.")
            rows = [notice, request, request, error, error, child, real_child, child_error, setup, other,
                    frame('user', [{'type': 'tool_result', 'tool_use_id': 'b', 'is_error': False,
                                    'content': 'process exited with status 1'}])]
            path.write_text(''.join(json.dumps(r) + '\n' for r in rows))
            self.assertEqual(protocol_tool_cost(path, 's'), {'tool_rounds': 1, 'tool_rounds_before_first_read': None, 'tool_requests': 2, 'repeated_tool_requests': None, 'tool_result_errors': 1, 'context_snapshot_polls': 0, 'same_image_rechecks': 0, 'image_rate_limits': 0})
            request['message']['message']['id'] = 'parallel-batch'
            for block in request['message']['message']['content']:
                block.update(name='read', input={'path': 'example.py'})
            path.write_text(''.join(json.dumps(r) + '\n' for r in rows))
            self.assertEqual(protocol_tool_cost(path, 's')['repeated_tool_requests'], 1)
            self.assertEqual(protocol_tool_cost(path, 's')['tool_rounds_before_first_read'], 0)
            # Production headless splits parallel starts into distinct message IDs.
            first = frame('assistant', [{'type': 'tool_use', 'id': 'a', 'name': 'list', 'input': {}}])
            first['message']['message']['id'] = 'first-frame'
            second = frame('assistant', [{'type': 'tool_use', 'id': 'b', 'name': 'read', 'input': {'path': 'file'}}])
            second['message']['message']['id'] = 'second-frame'
            for sequence, expected in [([first, second, error, rows[-1]], 0),
                                       ([first, error, second, rows[-1]], 1)]:
                path.write_text(''.join(json.dumps(r) + '\n' for r in sequence))
                cost = protocol_tool_cost(path, 's')
                self.assertEqual(cost['tool_rounds_before_first_read'], expected)
                self.assertEqual(cost['tool_rounds'], expected + 1)
            rows.append({'direction': 'host', 'message': {'type': 'control_request', 'request': {'subtype': 'context.snapshot'}}})
            path.write_text(''.join(json.dumps(r) + '\n' for r in rows))
            self.assertEqual(protocol_tool_cost(path, 's')['context_snapshot_polls'], 1)
            self.assertIsNone(protocol_tool_cost(path, None)['tool_requests'])
            self.assertIsNone(protocol_tool_cost(path.with_name('missing'), 's')['tool_requests'])
            path.write_text('{broken')
            self.assertIsNone(protocol_tool_cost(path, 's')['tool_requests'])

    def test_runtime_counts_preserve_compatibility_and_unknown(self):
        with tempfile.TemporaryDirectory() as root:
            t = Telemetry(root, 's')
            self.addCleanup(t.close)
            self.assertIsNone(t.cost()['tool_started'])
            journal = Path(root)/'sessions/s/runs/run/events.jsonl'
            journal.parent.mkdir(parents=True)
            journal.write_text('{"id":"a","type":"tool_started"}\n{"id":"b","type":"tool_blocked"}\n')
            t.refresh()
            self.assertEqual(t.cost()['tool_started'], 1)
            self.assertEqual(t.cost()['tool_calls'], 1)
            self.assertEqual(t.cost()['tool_blocked'], 1)

class ImageUsageCompletenessTests(unittest.TestCase):
    def test_image_unknown_usage_preserves_known_subtotal_and_attempt_count(self):
        t = Telemetry('/missing')
        t.calls = {'main': {'source': 'main', 'in': 100, 'out': 20, 'total': 120},
                   'image': {'source': 'image_understand', 'in': 0, 'out': 0, 'total': 0,
                             'usage_reported': False, 'tool_call_id': 'tool-image'}}
        cost = t.cost()
        self.assertEqual(cost['model_calls'], 2)
        self.assertEqual(cost['tool_model_calls'], 1)
        self.assertEqual(cost['usage_unknown_calls'], 1)
        self.assertIsNone(cost['total_tokens'])
        self.assertEqual(cost['observed_total_tokens'], 120)
        self.assertEqual(t.safe_calls()[1]['tool_call_id'], 'tool-image')
        t.calls['image'].update(usage_reported=True, total=30, **{'in': 20, 'out': 10})
        self.assertEqual(t.cost()['total_tokens'], 150)
        self.assertEqual(t.cost()['observed_total_tokens'], 150)

if __name__=='__main__':unittest.main()

class CoordinationAuditTests(unittest.TestCase):
    def test_coordinator_read_errors_and_unknown_findings_stay_visible(self):
        from agent_eval.telemetry import coordination_audit
        events=[{'type':'tool_completed','run_id':'coord','payload':{'tool_name':'task_result','is_error':True}},
                {'type':'tool_completed','run_id':'worker','payload':{'tool_name':'shell','is_error':False}},
                {'type':'todos_updated','payload':[]}]
        state={'tasks':[{'id':'old','status':'partial'}, {'id':'review','status':'completed','findings_reported':True,'findings':[
            {'disposition':'accepted'},{'disposition':'unverified'},{'disposition':'unreviewed'}]}]}
        audit=coordination_audit(events,state)
        self.assertEqual(audit['all_runtime_tool_errors'],1)
        self.assertEqual(audit['task_result_read_errors'],1)
        self.assertNotIn('legacy_review',audit)
        self.assertNotIn('terminal_tasks_without_structured_findings',audit)
        self.assertIsNone(coordination_audit([],None)['handoff_outcomes'])
        generic=coordination_audit(events,{'tasks':[{'id':'dev','status':'partial','review':{'outcome':'continue'}}]})
        self.assertNotIn('legacy_review',generic)
        self.assertEqual(generic['handoff_outcomes']['continue'],1)
