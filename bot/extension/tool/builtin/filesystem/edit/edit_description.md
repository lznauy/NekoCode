Edit a file by replacing text anchored to the file's current content.

Parameters:
- path: absolute file path.
- oldString: exact text currently in the file.
- newString: replacement text. Use an empty string to delete oldString.
- replaceAll: optional. Defaults to false.

Default behavior:
- oldString must match exactly once.
- If oldString matches multiple times, include more surrounding context.
- If oldString is not found, edit only tries conservative fallback matching for line endings, surrounding blank lines, and line-trimmed text.
- replaceAll=true replaces every exact match and does not use fallback matching.

Prefer copying enough unchanged context into oldString to make the match unique. Do not use line numbers, read windows, revision tags, or structured edit intents.

Constraints:
- Read the current file version before editing. An oldString anchor alone does not replace a read. Read again after any modification or context compaction; if output is truncated, read smaller ranges.
- Writes are committed atomically: a failed edit leaves the original file unchanged. Use `/rewind` to undo a completed edit or conversation turn.
