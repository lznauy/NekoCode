"""Conduit Lite: a small stdlib-only article browsing demo.

Design notes
------------
* Data lives in memory (``ARTICLES``); there is no database and no persistence
  across restarts.
* Authentication is a fixed demo-identity check: the ``Authorization`` header
  must carry one of ``alice-demo`` / ``bob-demo`` (``Token <id>``,
  ``Bearer <id>`` or the bare id).  No real auth is claimed.
* ``reset_state()`` restores the seed data so tests can share the process
  without leaking state between cases.

Run: ``python3 app.py --port 8080`` and open http://127.0.0.1:8080/
"""
import argparse
import copy
import json
import threading
from datetime import datetime, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, unquote, urlsplit

# Demo identity -> author username.  Fixed local fixtures, not production auth.
DEMO_IDENTITIES = {"alice-demo": "alice", "bob-demo": "bob"}

DEFAULT_LIMIT = 20
MIN_LIMIT = 1
MAX_LIMIT = 100
MIN_OFFSET = 0

# Fields a client may change through PUT /api/articles/{slug}.
MUTABLE_FIELDS = ("title", "description", "body")

SEED_ARTICLES = [
    {"slug": "new", "title": "New post", "description": "Go and API", "body": "New article", "tagList": ["go", "api"], "author": {"username": "alice"}, "createdAt": "2026-11-13T02:00:00Z", "updatedAt": "2026-11-13T02:00:00Z", "favoritesCount": 0, "favorited": False},
    {"slug": "middle", "title": "Middle post", "description": "Python", "body": "Middle article", "tagList": ["python"], "author": {"username": "bob"}, "createdAt": "2026-11-12T03:00:00Z", "updatedAt": "2026-11-12T03:00:00Z", "favoritesCount": 0, "favorited": False},
    {"slug": "old", "title": "Old post", "description": "Go", "body": "Old article", "tagList": ["go", "testing"], "author": {"username": "bob"}, "createdAt": "2026-11-11T03:00:00Z", "updatedAt": "2026-11-11T03:00:00Z", "favoritesCount": 0, "favorited": False},
]

# Live in-memory store; every read/write goes through _LOCK so the threading
# HTTP server cannot interleave a list read with an update write.
ARTICLES = copy.deepcopy(SEED_ARTICLES)
_LOCK = threading.RLock()


def reset_state():
    """Restore the seed data (used by tests to isolate shared state)."""
    with _LOCK:
        ARTICLES[:] = copy.deepcopy(SEED_ARTICLES)


class QueryError(ValueError):
    """Raised for invalid query parameters; mapped to HTTP 422."""


class RequestError(ValueError):
    """Raised for a malformed request body; mapped to HTTP 400."""


def _single_param(query, name):
    """Return the single value of ``name`` or None when absent/blank.

    An empty value (``?tag=``) means "no filter" and a repeated parameter
    (``?tag=a&tag=b``) is rejected instead of silently picking a winner.
    """
    values = query.get(name)
    if not values:
        return None
    if len(values) > 1:
        raise QueryError("repeated query parameter: %s" % name)
    value = values[0]
    if not isinstance(value, str):
        raise QueryError("invalid query parameter: %s" % name)
    value = value.strip()
    return value or None


def _int_param(query, name, default, minimum, maximum=None):
    """Strictly parse an integer query parameter.

    Missing -> ``default``.  Present but empty, non-numeric, fractional or
    out of range -> :class:`QueryError` (never a silent fallback).  Booleans
    and floats are rejected because only plain decimal integers are accepted.
    """
    values = query.get(name)
    if not values:
        return default
    if len(values) > 1:
        raise QueryError("repeated query parameter: %s" % name)
    raw = values[0]
    if not isinstance(raw, str):
        raise QueryError("invalid query parameter: %s" % name)
    raw = raw.strip()
    if not raw:
        raise QueryError("%s must not be empty" % name)
    try:
        value = int(raw, 10)
    except ValueError:
        raise QueryError("%s must be an integer" % name) from None
    if value < minimum:
        raise QueryError("%s must be >= %d" % (name, minimum))
    if maximum is not None and value > maximum:
        raise QueryError("%s must be <= %d" % (name, maximum))
    return value


def list_articles(query):
    """Filter -> sort -> paginate, returning articles plus the match count.

    Order matters: filters are applied to the whole collection first (so
    ``articlesCount`` is the number of matches, and a page never hides
    matches), sorting happens on the filtered set, and only then is the page
    sliced.  Articles are deep-copied so callers can never mutate the store
    and every article keeps its complete ``tagList``.
    """
    limit = _int_param(query, "limit", DEFAULT_LIMIT, MIN_LIMIT, MAX_LIMIT)
    offset = _int_param(query, "offset", MIN_OFFSET, MIN_OFFSET)
    tag = _single_param(query, "tag")
    author = _single_param(query, "author")

    with _LOCK:
        rows = copy.deepcopy(ARTICLES)

    if tag is not None:
        rows = [a for a in rows if tag in a.get("tagList", [])]
    if author is not None:
        rows = [a for a in rows if a.get("author", {}).get("username") == author]

    # Newest first; Python's sort is stable, so equal timestamps keep seed order.
    rows.sort(key=lambda a: a.get("createdAt", ""), reverse=True)

    total = len(rows)
    page = rows[offset:offset + limit]
    return {"articles": page, "articlesCount": total}


def _utc_now():
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def update_article(slug, actor, changes):
    """Update an article's mutable fields on behalf of ``actor``.

    Authorization is decided *before* anything is written: an unauthenticated
    caller gets 401, an unknown slug 404, a non-author 403, and only then is
    the payload validated (422) and applied.  A rejected request therefore
    leaves the stored article byte-for-byte unchanged.
    """
    with _LOCK:
        if not actor:
            return 401, {"errors": {"body": ["authentication required"]}}

        article = next((a for a in ARTICLES if a["slug"] == slug), None)
        if article is None:
            return 404, {"errors": {"body": ["not found"]}}

        if article.get("author", {}).get("username") != actor:
            return 403, {"errors": {"body": ["not the author"]}}

        if not isinstance(changes, dict):
            return 422, {"errors": {"article": ["must be an object"]}}
        for field in MUTABLE_FIELDS:
            if field in changes and not isinstance(changes[field], str):
                return 422, {"errors": {field: ["must be a string"]}}

        for field in MUTABLE_FIELDS:
            if field in changes:
                article[field] = changes[field]

        # Keep updatedAt monotonic even though the fixtures live in the future.
        now = _utc_now()
        if now > article.get("updatedAt", ""):
            article["updatedAt"] = now

        return 200, {"article": copy.deepcopy(article)}


def actor_from_authorization(header_value):
    """Map an Authorization header to an author username ("" when invalid)."""
    if not header_value:
        return ""
    parts = header_value.split()
    if len(parts) == 2 and parts[0].lower() in ("token", "bearer"):
        token = parts[1]
    elif len(parts) == 1:
        token = parts[0]
    else:
        return ""
    return DEMO_IDENTITIES.get(token, "")


def parse_put_body(raw):
    """Decode the JSON request body into the ``article`` payload object."""
    try:
        payload = json.loads(raw.decode("utf-8") or "")
    except (UnicodeDecodeError, json.JSONDecodeError):
        raise RequestError("request body must be valid JSON") from None
    if not isinstance(payload, dict):
        raise RequestError("request body must be a JSON object")
    changes = payload.get("article")
    if changes is None:
        raise RequestError("missing article object")
    if not isinstance(changes, dict):
        raise RequestError("article must be an object")
    return changes


class Handler(BaseHTTPRequestHandler):
    server_version = "ConduitLite/1.0"

    def _send(self, code, body, content_type="application/json; charset=utf-8"):
        data = body if isinstance(body, bytes) else json.dumps(body).encode()
        self.send_response(code)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        u = urlsplit(self.path)
        if u.path == "/api/articles":
            try:
                self._send(200, list_articles(parse_qs(u.query, keep_blank_values=True)))
            except QueryError as exc:
                self._send(422, {"errors": {"query": [str(exc)]}})
        elif u.path in ("/", "/index.html"):
            try:
                data = Path(__file__).with_name("index.html").read_bytes()
            except OSError:
                self._send(500, {"errors": {"body": ["index.html missing"]}})
                return
            self._send(200, data, "text/html; charset=utf-8")
        else:
            self._send(404, {"errors": {"body": ["not found"]}})

    def do_PUT(self):
        u = urlsplit(self.path)
        prefix = "/api/articles/"
        if not u.path.startswith(prefix) or u.path == prefix:
            self._send(404, {"errors": {"body": ["not found"]}})
            return
        slug = unquote(u.path[len(prefix):])

        # Identity check happens in update_article() so the ordering rules live
        # in one place (and are unit-testable); a missing/unknown token -> 401.
        actor = actor_from_authorization(self.headers.get("Authorization"))

        try:
            length = int(self.headers.get("Content-Length") or 0)
        except ValueError:
            self._send(400, {"errors": {"body": ["invalid Content-Length"]}})
            return
        if length <= 0:
            self._send(400, {"errors": {"body": ["request body required"]}})
            return
        try:
            changes = parse_put_body(self.rfile.read(length))
        except RequestError as exc:
            self._send(400, {"errors": {"body": [str(exc)]}})
            return

        code, value = update_article(slug, actor, changes)
        self._send(code, value)


def make_server(host="127.0.0.1", port=8080):
    return ThreadingHTTPServer((host, port), Handler)


def main(argv=None):
    p = argparse.ArgumentParser(description="Conduit Lite demo server")
    p.add_argument("--port", type=int, default=8080)
    p.add_argument("--host", default="127.0.0.1")
    a = p.parse_args(argv)
    httpd = make_server(a.host, a.port)
    print("Conduit Lite listening on http://%s:%d/" % (a.host, httpd.server_port))
    try:
        httpd.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        httpd.server_close()


if __name__ == "__main__":
    main()
