"""Regression tests for Conduit Lite (stdlib unittest only).

Every test drives the real HTTP surface: a real ThreadingHTTPServer is started
on an ephemeral loopback port and requests go through urllib, so the routing,
header handling, JSON encoding and status codes are all exercised.

Run: python3 -m unittest -v test_app.py
"""
import copy
import json
import threading
import unittest
import urllib.error
import urllib.request

import app as conduit


class ServerTestCase(unittest.TestCase):
    """Boots one server per class, resets the in-memory store per test."""

    @classmethod
    def setUpClass(cls):
        cls.httpd = conduit.make_server("127.0.0.1", 0)
        cls.port = cls.httpd.server_port
        cls.thread = threading.Thread(target=cls.httpd.serve_forever, daemon=True)
        cls.thread.start()

    @classmethod
    def tearDownClass(cls):
        cls.httpd.shutdown()
        cls.httpd.server_close()
        cls.thread.join(timeout=5)

    def setUp(self):
        conduit.reset_state()

    # -- helpers ---------------------------------------------------------
    def request(self, path, method="GET", body=None, headers=None, raw_body=None):
        url = "http://127.0.0.1:%d%s" % (self.port, path)
        data = raw_body
        if data is None and body is not None:
            data = json.dumps(body).encode("utf-8")
        req = urllib.request.Request(url, data=data, method=method)
        for key, value in (headers or {}).items():
            req.add_header(key, value)
        if data is not None:
            req.add_header("Content-Type", "application/json")
        try:
            with urllib.request.urlopen(req, timeout=10) as resp:
                payload = resp.read()
                return resp.status, self._decode(payload)
        except urllib.error.HTTPError as exc:
            return exc.code, self._decode(exc.read())

    @staticmethod
    def _decode(raw):
        try:
            return json.loads(raw.decode("utf-8"))
        except (UnicodeDecodeError, json.JSONDecodeError):
            return raw

    def slugs(self, path):
        status, payload = self.request(path)
        self.assertEqual(status, 200, payload)
        return [a["slug"] for a in payload["articles"]]

    def stored(self, slug):
        with conduit._LOCK:
            for article in conduit.ARTICLES:
                if article["slug"] == slug:
                    return copy.deepcopy(article)
        self.fail("article %r not found in store" % slug)


class ListArticlesTests(ServerTestCase):
    def test_default_limit_is_20_and_sorted_newest_first(self):
        status, payload = self.request("/api/articles")
        self.assertEqual(status, 200)
        self.assertEqual([a["slug"] for a in payload["articles"]], ["new", "middle", "old"])
        self.assertEqual(payload["articlesCount"], 3)

    def test_tag_filter_is_exact_and_keeps_full_tag_list(self):
        status, payload = self.request("/api/articles?tag=go")
        self.assertEqual(status, 200)
        self.assertEqual([a["slug"] for a in payload["articles"]], ["new", "old"])
        self.assertEqual(payload["articlesCount"], 2)
        # The tag filter must not truncate unrelated tags.
        self.assertEqual(payload["articles"][0]["tagList"], ["go", "api"])
        self.assertEqual(payload["articles"][1]["tagList"], ["go", "testing"])

    def test_tag_filter_is_not_a_substring_match(self):
        status, payload = self.request("/api/articles?tag=g")
        self.assertEqual(status, 200)
        self.assertEqual(payload["articles"], [])
        self.assertEqual(payload["articlesCount"], 0)

    def test_author_filter_is_exact(self):
        status, payload = self.request("/api/articles?author=bob")
        self.assertEqual(status, 200)
        self.assertEqual([a["slug"] for a in payload["articles"]], ["middle", "old"])
        self.assertEqual(payload["articlesCount"], 2)

        status, payload = self.request("/api/articles?author=bo")
        self.assertEqual(status, 200)
        self.assertEqual(payload["articlesCount"], 0)

    def test_author_and_tag_combine(self):
        status, payload = self.request("/api/articles?tag=go&author=bob")
        self.assertEqual(status, 200)
        self.assertEqual([a["slug"] for a in payload["articles"]], ["old"])
        self.assertEqual(payload["articlesCount"], 1)

    def test_combined_filters_with_no_match(self):
        status, payload = self.request("/api/articles?tag=python&author=alice")
        self.assertEqual(status, 200)
        self.assertEqual(payload["articles"], [])
        self.assertEqual(payload["articlesCount"], 0)

    def test_blank_tag_and_author_mean_no_filter(self):
        # Documented convention: an empty value is "no filter", not an error.
        status, payload = self.request("/api/articles?tag=&author=")
        self.assertEqual(status, 200)
        self.assertEqual(payload["articlesCount"], 3)

    def test_pagination_slices_after_sort_and_keeps_total_count(self):
        status, payload = self.request("/api/articles?limit=2")
        self.assertEqual(status, 200)
        self.assertEqual([a["slug"] for a in payload["articles"]], ["new", "middle"])
        self.assertEqual(payload["articlesCount"], 3)  # matches, not page size

        status, payload = self.request("/api/articles?limit=2&offset=2")
        self.assertEqual(status, 200)
        self.assertEqual([a["slug"] for a in payload["articles"]], ["old"])
        self.assertEqual(payload["articlesCount"], 3)

        status, payload = self.request("/api/articles?limit=1&offset=1")
        self.assertEqual(status, 200)
        self.assertEqual([a["slug"] for a in payload["articles"]], ["middle"])
        self.assertEqual(payload["articlesCount"], 3)

    def test_filters_apply_before_pagination(self):
        # Filtering after slicing (the original bug) returned a short/empty page
        # with a page-sized count.
        status, payload = self.request("/api/articles?author=bob&limit=2")
        self.assertEqual(status, 200)
        self.assertEqual([a["slug"] for a in payload["articles"]], ["middle", "old"])
        self.assertEqual(payload["articlesCount"], 2)

        status, payload = self.request("/api/articles?author=bob&limit=1")
        self.assertEqual(status, 200)
        self.assertEqual([a["slug"] for a in payload["articles"]], ["middle"])
        self.assertEqual(payload["articlesCount"], 2)

        status, payload = self.request("/api/articles?tag=go&limit=1&offset=1")
        self.assertEqual(status, 200)
        self.assertEqual([a["slug"] for a in payload["articles"]], ["old"])
        self.assertEqual(payload["articlesCount"], 2)

    def test_offset_past_end_returns_empty_page_with_real_count(self):
        status, payload = self.request("/api/articles?offset=99")
        self.assertEqual(status, 200)
        self.assertEqual(payload["articles"], [])
        self.assertEqual(payload["articlesCount"], 3)

    def test_limit_bounds_are_inclusive(self):
        for limit in ("1", "100"):
            status, payload = self.request("/api/articles?limit=%s" % limit)
            self.assertEqual(status, 200, "limit=%s -> %s" % (limit, payload))

    def test_repeated_queries_do_not_pollute_state(self):
        first = self.request("/api/articles?tag=go")[1]
        second = self.request("/api/articles?tag=go")[1]
        self.assertEqual(first, second)
        plain = self.request("/api/articles")[1]
        self.assertEqual([a["tagList"] for a in plain["articles"]],
                         [["go", "api"], ["python"], ["go", "testing"]])
        self.assertEqual(plain["articlesCount"], 3)
        # Seed data untouched on disk/store.
        self.assertEqual(conduit.ARTICLES, conduit.SEED_ARTICLES)

    def test_response_is_a_copy_of_the_store(self):
        status, payload = self.request("/api/articles")
        self.assertEqual(status, 200)
        payload["articles"][0]["tagList"].append("injected")
        payload["articles"][0]["body"] = "mutated by client"
        self.assertEqual(self.stored("new")["tagList"], ["go", "api"])
        self.assertEqual(self.stored("new")["body"], "New article")

    def test_unknown_path_is_404(self):
        status, _ = self.request("/api/nope")
        self.assertEqual(status, 404)

    def test_index_page_is_served(self):
        status, body = self.request("/")
        self.assertEqual(status, 200)
        html = body.decode("utf-8") if isinstance(body, bytes) else str(body)
        self.assertIn("Conduit Lite", html)
        self.assertIn("textContent", html)
        self.assertIn("/api/articles", html)
        # User content must never be assembled via innerHTML.
        self.assertNotIn("innerHTML", html)


class PaginationValidationTests(ServerTestCase):
    def assert_422(self, query):
        status, payload = self.request("/api/articles?" + query)
        self.assertEqual(status, 422, "%s -> %s %s" % (query, status, payload))
        self.assertIn("errors", payload)
        # A rejected request must not corrupt anything.
        self.assertEqual(conduit.ARTICLES, conduit.SEED_ARTICLES)

    def test_limit_out_of_range(self):
        for query in ("limit=0", "limit=-1", "limit=101", "limit=1000"):
            self.assert_422(query)

    def test_limit_not_an_integer(self):
        for query in ("limit=abc", "limit=1.5", "limit=2abc", "limit=1e2", "limit="):
            self.assert_422(query)

    def test_offset_out_of_range(self):
        for query in ("offset=-1", "offset=-100"):
            self.assert_422(query)

    def test_offset_not_an_integer(self):
        for query in ("offset=abc", "offset=1.5", "offset=", "offset=0x10"):
            self.assert_422(query)

    def test_blank_missing_is_not_silently_defaulted(self):
        # limit present-but-blank must be rejected, not coerced to 20.
        status, _ = self.request("/api/articles?limit=")
        self.assertEqual(status, 422)


class UpdateArticleTests(ServerTestCase):
    def put(self, slug, body, token=None, raw_body=None):
        headers = {}
        if token is not None:
            headers["Authorization"] = "Token " + token
        return self.request("/api/articles/" + slug, method="PUT",
                            body=body, raw_body=raw_body, headers=headers)

    def test_author_can_update_body_and_it_persists(self):
        status, payload = self.put("new", {"article": {"body": "Alice edited this"}},
                                   token="alice-demo")
        self.assertEqual(status, 200, payload)
        self.assertEqual(payload["article"]["body"], "Alice edited this")
        self.assertEqual(self.stored("new")["body"], "Alice edited this")
        # And the change is visible through the list endpoint.
        listed = self.request("/api/articles")[1]
        self.assertEqual({a["slug"]: a["body"] for a in listed["articles"]}["new"],
                         "Alice edited this")

    def test_author_can_update_multiple_fields(self):
        status, payload = self.put(
            "middle",
            {"article": {"body": "new body", "title": "New title", "description": "New desc"}},
            token="bob-demo",
        )
        self.assertEqual(status, 200, payload)
        stored = self.stored("middle")
        self.assertEqual(stored["body"], "new body")
        self.assertEqual(stored["title"], "New title")
        self.assertEqual(stored["description"], "New desc")
        self.assertEqual(stored["slug"], "middle")
        self.assertEqual(stored["tagList"], ["python"])

    def test_bearer_and_bare_demo_tokens_are_accepted(self):
        for header in ("Bearer alice-demo", "alice-demo"):
            conduit.reset_state()
            status, _ = self.request(
                "/api/articles/new", method="PUT",
                body={"article": {"body": "via " + header}},
                headers={"Authorization": header},
            )
            self.assertEqual(status, 200, header)

    def test_missing_identity_is_401_and_writes_nothing(self):
        before = copy.deepcopy(conduit.ARTICLES)
        status, payload = self.put("new", {"article": {"body": "anon"}})
        self.assertEqual(status, 401, payload)
        self.assertEqual(conduit.ARTICLES, before)

    def test_invalid_identity_is_401_and_writes_nothing(self):
        for token in ("carol-demo", "alice", "", "Token alice-demo extra"):
            before = copy.deepcopy(conduit.ARTICLES)
            status, payload = self.put("new", {"article": {"body": "nope"}}, token=token)
            self.assertEqual(status, 401, "%r -> %s %s" % (token, status, payload))
            self.assertEqual(conduit.ARTICLES, before)

    def test_non_author_is_403_and_state_is_unchanged(self):
        before = copy.deepcopy(conduit.ARTICLES)
        status, payload = self.put("new", {"article": {"body": "HACKED by bob"}},
                                   token="bob-demo")
        self.assertEqual(status, 403, payload)
        self.assertEqual(conduit.ARTICLES, before)
        self.assertEqual(self.stored("new")["body"], "New article")
        # Nothing leaks through the read path either.
        listed = self.request("/api/articles")[1]
        self.assertEqual({a["slug"]: a["body"] for a in listed["articles"]}["new"],
                         "New article")

    def test_non_author_is_403_for_own_article_of_other_bob_post(self):
        before = copy.deepcopy(conduit.ARTICLES)
        status, _ = self.put("middle", {"article": {"title": "hijack"}}, token="alice-demo")
        self.assertEqual(status, 403)
        self.assertEqual(conduit.ARTICLES, before)

    def test_unknown_slug_is_404(self):
        before = copy.deepcopy(conduit.ARTICLES)
        status, payload = self.put("does-not-exist", {"article": {"body": "x"}},
                                   token="alice-demo")
        self.assertEqual(status, 404, payload)
        self.assertEqual(conduit.ARTICLES, before)

    def test_invalid_field_type_is_422_and_writes_nothing(self):
        before = copy.deepcopy(conduit.ARTICLES)
        status, payload = self.put("new", {"article": {"body": 123}}, token="alice-demo")
        self.assertEqual(status, 422, payload)
        self.assertEqual(conduit.ARTICLES, before)

    def test_malformed_body_is_400_and_writes_nothing(self):
        before = copy.deepcopy(conduit.ARTICLES)
        for raw in (b"{not json", b"[]", b'{"body":"no wrapper"}', b'{"article":[]}'):
            status, payload = self.put("new", None, token="alice-demo", raw_body=raw)
            self.assertEqual(status, 400, "%r -> %s %s" % (raw, status, payload))
            self.assertEqual(conduit.ARTICLES, before)

    def test_put_to_unrelated_path_is_404(self):
        status, _ = self.put("", {"article": {"body": "x"}}, token="alice-demo")
        self.assertEqual(status, 404)

    def test_updated_at_does_not_move_backwards(self):
        before = self.stored("new")["updatedAt"]
        self.put("new", {"article": {"body": "b"}}, token="alice-demo")
        self.assertGreaterEqual(self.stored("new")["updatedAt"], before)


class IsolationTests(ServerTestCase):
    def test_reset_state_restores_seed_data(self):
        self.put_update()
        self.assertNotEqual(conduit.ARTICLES, conduit.SEED_ARTICLES)
        conduit.reset_state()
        self.assertEqual(conduit.ARTICLES, conduit.SEED_ARTICLES)

    def put_update(self):
        status, _ = self.request(
            "/api/articles/new", method="PUT",
            body={"article": {"body": "temporary"}},
            headers={"Authorization": "Token alice-demo"},
        )
        self.assertEqual(status, 200)

    def test_seed_data_is_not_shared_by_reference(self):
        conduit.ARTICLES[0]["tagList"].append("leak")
        self.assertNotIn("leak", conduit.SEED_ARTICLES[0]["tagList"])
        conduit.reset_state()
        self.assertEqual(conduit.ARTICLES[0]["tagList"], ["go", "api"])


if __name__ == "__main__":
    unittest.main()
