"""Evaluator-owned acceptance probes, not copied into the agent workspace."""
import copy
import importlib.util
import json
import pathlib
import subprocess
import sys
import tempfile
import unittest
import urllib.request
import urllib.error
import socket
import time

ROOT = pathlib.Path(sys.argv.pop(1)).resolve()
PRODUCT_ERROR = None
try:
    spec = importlib.util.spec_from_file_location("demo_app", ROOT / "app.py")
    app = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(app)
    SEED = copy.deepcopy(app.ARTICLES)
except (Exception, SystemExit) as exc:
    # The candidate is test input. Its import/state failure is a failed
    # product contract, not a failure to load the evaluator's own code.
    PRODUCT_ERROR = f"Candidate app.py could not load: {type(exc).__name__}: {exc}"

class Contract(unittest.TestCase):
    def setUp(self):
        if PRODUCT_ERROR is not None:
            self.fail(PRODUCT_ERROR)
        app.ARTICLES[:] = copy.deepcopy(SEED)

    def test_total_before_pagination(self):
        r = app.list_articles({"limit": ["1"], "offset": ["1"]})
        self.assertEqual(r["articlesCount"], 3)
        self.assertEqual([a["slug"] for a in r["articles"]], ["middle"])

    def test_filter_before_pagination(self):
        r = app.list_articles({"tag": ["go"], "limit": ["1"], "offset": ["1"]})
        self.assertEqual(r["articlesCount"], 2)
        self.assertEqual([a["slug"] for a in r["articles"]], ["old"])

    def test_combined_exact_filters(self):
        r = app.list_articles({"tag": ["go"], "author": ["bob"], "limit": ["1"]})
        self.assertEqual(r["articlesCount"], 1)
        self.assertEqual([a["slug"] for a in r["articles"]], ["old"])
        self.assertEqual(app.list_articles({"tag": ["g"]})["articlesCount"], 0)

    def test_preserve_tags_and_state(self):
        r = app.list_articles({"tag": ["go"]})
        self.assertEqual(r["articles"][0]["tagList"], ["go", "api"])
        r["articles"][0]["tagList"].append("external")
        self.assertEqual(app.ARTICLES, SEED)

    def test_empty_page_preserves_total(self):
        r = app.list_articles({"tag": ["go"], "offset": ["999"]})
        self.assertEqual(r, {"articles": [], "articlesCount": 2})

    def test_denied_write_preserves_state(self):
        code, _ = app.update_article("new", "bob", {"body": "poison"})
        self.assertEqual(code, 403)
        self.assertEqual(app.ARTICLES, SEED)

    def test_owner_update_works(self):
        code, r = app.update_article("new", "alice", {"body": "edited"})
        self.assertEqual(code, 200)
        self.assertEqual(r["article"]["body"], "edited")
        self.assertEqual(app.ARTICLES[0]["body"], "edited")

    def test_unauthenticated_update_preserves_state(self):
        self.assertEqual(app.update_article("new", "", {"body": "poison"})[0], 401)
        self.assertEqual(app.ARTICLES, SEED)

    def test_public_http_and_pagination_validation(self):
        with socket.socket() as s:
            s.bind(("127.0.0.1", 0)); port = s.getsockname()[1]
        p = subprocess.Popen([sys.executable, str(ROOT / "app.py"), "--port", str(port)], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        try:
            url = f"http://127.0.0.1:{port}"
            for _ in range(100):
                try:
                    urllib.request.urlopen(url + "/api/articles", timeout=.2).close(); break
                except OSError: time.sleep(.03)
            for query in ["limit=0", "limit=-1", "limit=101", "offset=-1", "limit=x", "offset=1.5"]:
                with self.subTest(query=query):
                    try:
                        response = urllib.request.urlopen(url + "/api/articles?" + query)
                        self.fail(f"invalid pagination accepted: {query}, status={response.status}")
                    except urllib.error.HTTPError as e: self.assertEqual(e.code, 422)
            data = json.load(urllib.request.urlopen(url + "/api/articles?tag=go&limit=1&offset=1"))
            self.assertEqual(data["articlesCount"], 2)
            self.assertEqual(data["articles"][0]["slug"], "old")
            req = urllib.request.Request(url + "/api/articles/new", data=b'{"article":{"body":"changed"}}', headers={"Authorization": "Token bob-demo", "Content-Type": "application/json"}, method="PUT")
            with self.assertRaises(urllib.error.HTTPError) as caught: urllib.request.urlopen(req)
            self.assertEqual(caught.exception.code, 403)
            data = json.load(urllib.request.urlopen(url + "/api/articles"))
            self.assertEqual(data["articles"][0]["body"], "New article")
            html = urllib.request.urlopen(url).read().decode()
            self.assertIn("<", html)
            self.assertNotIn("awaiting implementation", html)
        finally:
            p.terminate(); p.wait(timeout=5)

if __name__ == "__main__": unittest.main(verbosity=2)
