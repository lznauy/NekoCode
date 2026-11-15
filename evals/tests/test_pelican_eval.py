import importlib.util
import os
from pathlib import Path
import tempfile
import unittest

from agent_eval.catalog import ROOT, load_case, load_suite
from agent_eval.verification import verify

spec = importlib.util.spec_from_file_location('pelican_verify', ROOT/'cases/pelican-bicycle/verify.py')
checker = importlib.util.module_from_spec(spec)
spec.loader.exec_module(checker)
SVG = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 120 80">{}</svg>'


class PelicanTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)

    def checks(self, text):
        p = self.root/'pelican.svg'
        p.write_text(text)
        return {c['id']: c for c in checker.inspect_svg(p)}

    def test_manifest_and_visual_suite(self):
        suite, cases = load_suite('visual')
        self.assertEqual(suite['cases'], ['pelican-bicycle'])
        self.assertEqual(len(cases), 1)
        case = load_case('pelican-bicycle')
        self.assertEqual(sum(r['required'] for r in case['manifest']['requirements']), 4)
        self.assertEqual(len(case['manifest']['requirements']), 8)

    def test_invalid_and_missing_documents_cannot_pass(self):
        missing = checker.inspect_svg(self.root/'missing.svg')
        self.assertEqual(missing[0]['status'], 'failed')
        for text in ['', '<svg>', '<svg/>', SVG.replace('120 80', '0 80'),
                     SVG.replace('120 80', 'NaN 80'), '<!DOCTYPE svg>'+SVG.format(''),
                     '<?xml-stylesheet href="https://example.com/x.css"?>'+SVG.format('')]:
            self.assertEqual(self.checks(text)['svg_document']['status'], 'failed', text)

    def test_active_external_and_raster_content_rejected(self):
        for body in ['<script>alert(1)</script>', '<foreignObject/>', '<animate/>',
                     '<g onclick="alert(1)"/>', '<use href="https://example.com/x.svg#bird"/>',
                     '<g style="fill:url(https://example.com/a)"/>',
                     '<style>@import "https://example.com/a";</style>',
                     '<g xml:base="https://example.com/"/>',
                     '<style>&#64;import "https://example.com/x.css";</style>',
                     '<g style="fill:u&#114;l(https://example.com/a.svg#x)"/>']:
            self.assertEqual(self.checks(SVG.format(body))['self_contained']['status'], 'failed', body)
        self.assertEqual(self.checks(SVG.format('<image href="data:image/png;base64,eA=="/>'))['vector_only']['status'], 'failed')
        self.assertEqual(self.checks(SVG.format('<feImage/>'))['vector_only']['status'], 'failed')
        local = self.checks(SVG.format('<defs><linearGradient id="a"/></defs><rect fill="url(#a)"/>'))
        self.assertTrue(all(c['status']=='passed' for c in local.values()))

    def test_seed_is_not_a_product(self):
        case = load_case('pelican-bicycle')
        result = verify(case, case['paths']['fixture'], self.root/'seed-check')
        self.assertFalse(result['product_passed'])
        self.assertIsNone(result['error'])
        self.assertTrue(all(c['status']=='not_run' for c in result['checks'] if c['id'] in checker.VISUAL))

    @unittest.skipUnless(os.environ.get('NEKOCODE_EVAL_BROWSER_TESTS')=='1', 'optional real Chrome verifier self-check')
    def test_rendering_checks_do_not_claim_pelican_recognition(self):
        case = load_case('pelican-bicycle')
        workspace = self.root/'product'; workspace.mkdir()
        # A circle passes mechanical checks, while visual meaning stays unknown.
        p = workspace/'pelican.svg'
        p.write_text(SVG.format('<circle cx="60" cy="40" r="28" fill="red" stroke="black" stroke-width="4"/>'))
        result = verify(case, workspace, self.root/'circle')
        self.assertTrue(result['product_passed'], result)
        self.assertTrue((self.root/'circle/desktop.png').exists())
        self.assertTrue((self.root/'circle/mobile.png').exists())
        self.assertTrue(all(c['status']=='not_run' for c in result['checks'] if c['id'] in checker.VISUAL))
        for name, body in [('empty', ''), ('hidden', '<circle cx="60" cy="40" r="28" fill="red" visibility="hidden"/>')]:
            p.write_text(SVG.format(body))
            result = verify(case, workspace, self.root/name)
            self.assertIsNone(result['error'], result)
            self.assertFalse(result['product_passed'], result)
            self.assertEqual(next(c for c in result['checks'] if c['id']=='rendered')['status'], 'failed')


if __name__=='__main__':
    unittest.main()
