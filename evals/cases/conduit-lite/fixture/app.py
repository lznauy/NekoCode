"""Small intentionally faulty RealWorld-inspired article product (stdlib only)."""
import copy
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlsplit

ARTICLES = [
    {"slug": "new", "title": "New post", "description": "Go and API", "body": "New article", "tagList": ["go", "api"], "author": {"username": "alice"}, "createdAt": "2026-11-13T02:00:00Z", "updatedAt": "2026-11-13T02:00:00Z", "favoritesCount": 0, "favorited": False},
    {"slug": "middle", "title": "Middle post", "description": "Python", "body": "Middle article", "tagList": ["python"], "author": {"username": "bob"}, "createdAt": "2026-11-12T03:00:00Z", "updatedAt": "2026-11-12T03:00:00Z", "favoritesCount": 0, "favorited": False},
    {"slug": "old", "title": "Old post", "description": "Go", "body": "Old article", "tagList": ["go", "testing"], "author": {"username": "bob"}, "createdAt": "2026-11-11T03:00:00Z", "updatedAt": "2026-11-11T03:00:00Z", "favoritesCount": 0, "favorited": False},
]


def list_articles(query):
    rows = sorted(copy.deepcopy(ARTICLES), key=lambda a: a["createdAt"], reverse=True)
    limit = int(query.get("limit", ["20"])[0])
    offset = int(query.get("offset", ["0"])[0])
    rows = rows[offset:offset + limit]
    tag = query.get("tag", [""])[0]
    author = query.get("author", [""])[0]
    if tag:
        rows = [a for a in rows if tag in a["tagList"]]
        for article in rows:
            article["tagList"] = [tag]
    if author:
        rows = [a for a in rows if a["author"]["username"] == author]
    return {"articles": rows, "articlesCount": len(rows)}


def update_article(slug, actor, changes):
    if not actor:
        return 401, {"errors": {"body": ["authentication required"]}}
    for article in ARTICLES:
        if article["slug"] == slug:
            if "body" in changes:
                article["body"] = changes["body"]
            if actor != article["author"]["username"]:
                return 403, {"errors": {"body": ["not the author"]}}
            return 200, {"article": copy.deepcopy(article)}
    return 404, {"errors": {"body": ["not found"]}}


class Handler(BaseHTTPRequestHandler):
    def respond(self, code, value):
        data = json.dumps(value).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        u = urlsplit(self.path)
        if u.path == "/api/articles":
            try:
                self.respond(200, list_articles(parse_qs(u.query)))
            except (ValueError, OverflowError):
                self.respond(422, {"errors": {"query": ["invalid pagination"]}})
        elif u.path == "/":
            from pathlib import Path
            data = Path(__file__).with_name("index.html").read_bytes()
            self.send_response(200)
            self.send_header("Content-Type", "text/html; charset=utf-8")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)
        else:
            self.respond(404, {"errors": {"body": ["not found"]}})

    def do_PUT(self):
        slug = urlsplit(self.path).path.removeprefix("/api/articles/")
        body = json.loads(self.rfile.read(int(self.headers.get("Content-Length", "0"))))
        # Demo identities only; these are fixed local fixtures, not production auth.
        token = self.headers.get("Authorization", "").removeprefix("Token ")
        actor = {"alice-demo": "alice", "bob-demo": "bob"}.get(token, "")
        code, value = update_article(slug, actor, body.get("article", {}))
        self.respond(code, value)


if __name__ == "__main__":
    import argparse
    p = argparse.ArgumentParser()
    p.add_argument("--port", type=int, default=8080)
    a = p.parse_args()
    ThreadingHTTPServer(("127.0.0.1", a.port), Handler).serve_forever()
