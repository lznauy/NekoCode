COPY TAG AND LINE NUMBERS FROM READ OUTPUT. NEVER guess line numbers from memory — always look at the Read output you just received. RE-GROUND AFTER EVERY EDIT: stale tag → STOP and re-Read.


Named-line replacement/insertion/deletion DSL. Headers ending in : take + body rows; delete has no body.

<headers>
[ABSPATH#TAG] — copy/paste from Read output. TAG is REQUIRED (no hashless form). New files use write.
</headers>

<ops>
Every edit MUST include at least one operation line (replace/delete/insert). A [ABSPATH#TAG] header alone is not a valid edit.
replace N..M: — delete lines N..M (inclusive) first, then insert body at line N. Use for specific sub-block edits.
replace block N: — replace the whole syntactic block (function/class/etc) starting at N. Use this for whole constructs.
delete N..M — delete lines N..M, no body.
delete block N — delete the whole syntactic block starting at N.
insert before N: — insert body rows before line N
insert after N: — insert body rows after line N. When N is a structural closer (}), the tool may slide the insertion past it if your body indentation is shallower than the anchor — this is intentional and avoids nesting errors.
insert after block N: — insert after END of block starting at N. N must be the OPENER line (def/class/{), never the closer (}) — if you only see the closer, use plain insert after M: with the closer line number.
insert head: / insert tail: — insert at file start/end
Single-line edits: replace N..N: or delete N. Body length is irrelevant — replacing 1 line with 10 is still replace N..N:.
</ops>

<body-rows>
Every body row under a : header is:
  +TEXT — literal line (leading whitespace kept). + alone adds a blank line.
NEVER write -old or bare/context lines. The range does the deleting; the body is only the new content.
To insert a literal line starting with - or +: +-x, ++x.
</body-rows>

<example>
Original Read output:
  [/abs/path/greet.py#A1B2]
  1:def greet(name):
  2:    msg = "Hello, " + name
  3:    print(msg)
  4:greet("world")

Insert a guard after line 1:
  [/abs/path/greet.py#A1B2]
  insert after 1:
  +    if not name: name = "stranger"

Replace line 2 (1 line replaced by 2):
  [/abs/path/greet.py#A1B2]
  replace 2..2:
  +    greeting = "Hi"
  +    msg = f"{greeting}, {name}"

Delete line 3:
  [/abs/path/greet.py#A1B2]
  delete 3

Replace whole greet function (lines 1-3; line 4 is a separate statement):
  [/abs/path/greet.py#A1B2]
  replace block 1:
  +def greet(name):
  +    print(f"Hello, {name}")

Decorator/doc-comment is a separate block — anchor on the decorator to sweep both:
  [/abs/path/svc.py#C3D4]
  replace block 1:
  +@cache
  +def load(key):
  +    return store[key]
</example>

<anti-patterns>
# WRONG — empty replace to delete. RIGHT: delete 4
replace 4..4:

# WRONG — range describes post-edit size. RIGHT: replace 1..1:
replace 1..2:
+def greet(name):

# WRONG — - rows / bare context. The range deletes; body is only new content.
replace 3..3:
    msg = "Hello, " + name
-   print(msg)
+   return msg

# WRONG — pure insertion via widened replace (retypes keepers, drops one).
# RIGHT — touch nothing you keep:
insert after 2:
+    extra = compute(name)

# WRONG — head/tail with a line number. RIGHT: just insert head: or insert tail:
insert head 1:
+first line

# WRONG — insert after block N anchored on closer. RIGHT: plain insert after M:
insert after block 3:
+after()
</anti-patterns>

<rules>
- Line numbers and [PATH#TAG] come from your latest Read or edit response. They do NOT shift as hunks apply. RE-GROUND AFTER EVERY EDIT: every apply mints a fresh TAG with [path#TAG] + numbered lines for chaining. Stale tag? STOP, re-Read.
- Ranges cover ONLY lines whose content changes. Never widen over unchanged lines — a stale wide range shreds everything it spans.
- Whole construct (function/class/block) → replace block N. Specific lines inside a construct → replace N..M.
- Body must NOT repeat lines from the N..M range — the tool deletes them first, then inserts body. Repeating lines appears twice in the result.
- Alignment-sensitive content (ASCII art, table borders, diagrams): never single-line replace. Replace the entire block to preserve alignment.
- insert after block N: N is the OPENER line number, never the closer. If you only see the closer (}), use plain insert after M:.
- Elided regions (…) are UNSEEN — never place or span a hunk across one. Read it first.
- One hunk per range. Non-adjacent changes = separate hunks.
- Indent body rows exactly for their target depth.
- Undo mistake: call edit with revert=true, patch="/path/to/file". Reverts to the most recent pre-edit state only (one layer). For multiple undos, call revert repeatedly. After revert you MUST re-Read the file before making new edits — the file state (and line numbers) may differ from before the revert.
- NEVER format/restyle code; run the project formatter instead.
</rules>
