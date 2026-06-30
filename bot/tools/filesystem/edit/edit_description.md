Edit a file by replacing text anchored to the file's current content.

Parameters:
- path: absolute file path.
- oldString: exact text currently in the file.
- newString: replacement text. Use an empty string to delete oldString.
- replaceAll: optional. Defaults to false.
- revert: optional. Restores the file to its pre-edit snapshot; only path is needed.

Default behavior:
- oldString must match exactly once.
- If oldString matches multiple times, include more surrounding context.
- If oldString is not found, edit only tries conservative fallback matching for line endings, surrounding blank lines, and line-trimmed text.
- replaceAll=true replaces every exact match and does not use fallback matching.

Prefer copying enough unchanged context into oldString to make the match unique. Do not use line numbers, read windows, revision tags, or structured edit intents.

Constraints:
- edit/write before Read: the policy layer tracks reads and warns if a file is written without prior Read. Always Read the file first to confirm current content before editing.
- Snapshot: edit keeps only one latest pre-edit snapshot per file. Repeated revert restores this same snapshot until another edit records a new one.
