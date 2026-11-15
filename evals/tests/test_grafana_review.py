import importlib.util,json,tempfile,unittest
from pathlib import Path

ROOT=Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('grafana_verify',ROOT/'cases/grafana-anonymous-review/verify.py')
module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)

class GrafanaReviewTests(unittest.TestCase):
    def test_review_delivery_does_not_grade_keywords_as_accuracy(self):
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp);original=root/'seed';work=root/'work'
            for p in [original,work]:
                (p/'repo').mkdir(parents=True);(p/'repo/code.go').write_text('package example\n');(p/'repo/go.mod').write_text('module example\n');(p/'review.patch').write_text('fixed review diff\n')
            answer={'findings':[{'title':'a claim','file':'code.go','start_line':1,'end_line':1,'severity':'high','trigger':'concrete sequence','impact':'claimed impact','evidence':'source inspected','verification':'static','source_tasks':['task-one']}],'reviewed_scopes':['code.go'],'limitations':[]}
            baseline=module.source_digest(work)
            result=module.evaluate(work,json.dumps(answer),baseline)
            statuses={c['id']:c['status'] for c in result['checks']}
            self.assertEqual(statuses,{'structured_report':'passed','source_unchanged':'passed','valid_locations':'passed','review_accuracy':'not_run'})
            answer['findings'][0]['file']='../outside'
            self.assertEqual(module.evaluate(work,json.dumps(answer),baseline)['checks'][2]['status'],'failed')
            (work/'report.txt').write_text('unrequested write')
            self.assertEqual(module.evaluate(work,json.dumps(answer),baseline)['checks'][1]['status'],'failed')
            self.assertEqual(module.evaluate(work,'not json',baseline)['checks'][0]['status'],'failed')
