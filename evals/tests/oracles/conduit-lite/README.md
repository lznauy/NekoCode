# Conduit Lite

A tiny RealWorld-inspired article browser built with the **Python standard
library only** (no third-party dependencies, no build step). Data lives in
memory; restarting the server resets it to the seed fixtures.

## Run

```bash
cd /var/tmp/nekocode-harness-eval-final/current-workspace
python3 app.py --port 8080
```

Then open <http://127.0.0.1:8080/> (the server prints the URL on startup).
`--host` defaults to `127.0.0.1`; `--port` defaults to `8080`.

Stop the server with `Ctrl+C`.

## What the page does

* Lists articles newest first, **2 per page**, with Previous / Next.
* Filter by **tag** and **author** — both match exactly (case-sensitive,
  whole value, no substring matching) and can be combined.
* The matching total (`articlesCount`) is shown as "Showing N of M matching
  articles", independent of the current page.
* Changing a filter (Apply / Reset) always returns to page 1.
* Errors from the API are shown in a red alert box with the server's message
  (for example an out-of-range `limit`).
* Article text is inserted with `textContent` / `createElement` only, so
  article content can never inject markup.
* Deep links work: `http://127.0.0.1:8080/?tag=go&author=bob`.

Seed articles: `new` (alice, tags go+api), `middle` (bob, tag python),
`old` (bob, tags go+testing).

## Demo identities

There is **no registration or login**. Two fixed demo identities exist; the
`Authorization` header carries the demo id:

| Header                         | Acts as author |
| ------------------------------ | -------------- |
| `Authorization: Token alice-demo` | `alice`     |
| `Authorization: Token bob-demo`   | `bob`       |

`Bearer alice-demo` and the bare `alice-demo` are accepted too. Any other or
missing value is rejected with `401`. This is a fixed local fixture, not real
authentication.

Only the article's author may update it; anyone else gets `403` and the stored
article is left completely unchanged.

### Update an article body

```bash
# Works: alice owns slug "new"
curl -i -X PUT http://127.0.0.1:8080/api/articles/new \
  -H 'Authorization: Token alice-demo' \
  -H 'Content-Type: application/json' \
  -d '{"article": {"body": "Updated article body"}}'

# Rejected: 403, article unchanged
curl -i -X PUT http://127.0.0.1:8080/api/articles/new \
  -H 'Authorization: Token bob-demo' \
  -H 'Content-Type: application/json' \
  -d '{"article": {"body": "hijacked"}}'

# Rejected: 401, no/invalid demo identity
curl -i -X PUT http://127.0.0.1:8080/api/articles/new \
  -H 'Content-Type: application/json' \
  -d '{"article": {"body": "anonymous"}}'
```

The update applies `title`, `description` and `body` (string values only) and
refreshes `updatedAt`; the change is visible on the next `GET /api/articles`.

## API

### `GET /api/articles`

Query parameters, applied in this order: **filter → sort (`createdAt` desc) →
paginate**.

| Param    | Default | Rules                                                     |
| -------- | ------- | --------------------------------------------------------- |
| `tag`    | none    | exact match against `tagList`; blank means "no filter"    |
| `author` | none    | exact match against `author.username`; blank = no filter  |
| `limit`  | `20`    | integer `1..100`                                          |
| `offset` | `0`     | integer `>= 0`                                            |

* `articlesCount` is the number of articles matching the filters, **not** the
  number returned on this page.
* Filtering never trims an article's `tagList`; the full list is returned.
* Responses are deep copies, so a client can never mutate server state.
* Invalid pagination returns `422` with `{"errors": {"query": [...]}}`:
  non-integers (`abc`, `1.5`, `2abc`), present-but-blank (`limit=`), and
  out-of-range values (`limit=0`, `limit=101`, `offset=-1`) are all rejected
  rather than silently defaulted. A repeated parameter is also `422`.
* Unknown paths return `404`.

### `PUT /api/articles/{slug}`

Body: `{"article": {"body": "...", "title": "...", "description": "..."}}`.

| Situation                                   | Status |
| ------------------------------------------- | ------ |
| no / invalid demo identity                  | 401    |
| unknown slug                                | 404    |
| authenticated but not the author            | 403    |
| non-string field, or `article` not an object| 422    |
| malformed / missing JSON body               | 400    |
| author updates own article                  | 200    |

Authorization is checked **before** any write, so every failing path leaves the
stored article untouched.

## Tests

```bash
cd /var/tmp/nekocode-harness-eval-final/current-workspace
python3 -m unittest -v test_app.py
```

34 tests, standard library `unittest` only. Each test class starts a real
`ThreadingHTTPServer` on an ephemeral loopback port and drives it over HTTP via
`urllib`, so routing, headers, JSON encoding and status codes are all covered
(not just helper functions). `app.reset_state()` restores the seed data before
every test, so runs are repeatable and order-independent.

## Files

| File             | Purpose                                        |
| ---------------- | ---------------------------------------------- |
| `app.py`         | HTTP server, in-memory store, API logic        |
| `index.html`     | Article browser front end (vanilla JS)         |
| `test_app.py`    | Standard-library regression tests              |
| `spec_excerpt.md`| Local contract notes                           |
| `README.md`      | This file                                      |

## Known limits

* In-memory storage only: no persistence across restarts, no concurrency-safe
  multi-process story (a single process guards its store with a lock).
* Only list + update exist; no create, delete, comments, favorites, profiles or
  feeds.
* Demo identities are fixed fixtures; there is no password, session or token
  expiry — this is explicitly not production authentication.
