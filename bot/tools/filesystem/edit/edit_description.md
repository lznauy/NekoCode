Edit files using JSON intents derived from Read VIEW metadata.

Always Read the target range first. Read output contains:

[/abs/path/file.go#TAG]
VIEW rev=TAG window=W1_20_ab12cd lines=1..20 total=80
1:code

Use the VIEW rev as base_revision and the VIEW window as target.window_id. Do not copy hashes or VIEW text into file content.

JSON intent format:

{
  "path": "/abs/path/file.go",
  "base_revision": "TAG_FROM_READ",
  "ops": [
    {
      "op": "replace",
      "target": {"window_id": "W1_20_ab12cd", "start_line": 7, "end_line": 9},
      "content": "new exact text"
    }
  ]
}

Delete example (removes lines 7-9, no content field):

{
  "path": "/abs/path/file.go",
  "base_revision": "TAG_FROM_READ",
  "ops": [
    {
      "op": "delete",
      "target": {"window_id": "W1_20_ab12cd", "start_line": 7, "end_line": 9}
    }
  ]
}

Supported ops:
- replace: replace target lines with content (content is required and must be non-empty)
- delete: remove target lines entirely; do NOT include content field. Use this instead of replace with empty content.
- insert_before: insert content before start_line
- insert_after: insert content after end_line

Rules:
- One edit call targets one file. Multiple non-overlapping ops in that file are allowed.
- Every op must target lines inside the same Read window it references.
- Line numbers are the visible file line numbers from Read output.
- If another edit changed the same file but your target lines are unchanged, edit may safely rebase and apply.
- If edit reports conflict or unknown window, re-Read the target range and retry with fresh VIEW metadata.
- For revert, call edit with revert=true and patch set to the bare file path.
