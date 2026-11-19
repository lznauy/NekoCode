def advance(db, stream, limit, after_apply=lambda: None):
    cursor = db.execute("SELECT ts,id FROM progress WHERE stream=?", (stream,)).fetchone()
    rows = db.execute(
        "SELECT ts,id,delta FROM events WHERE stream=? AND (? IS NULL OR ts>?) ORDER BY ts,id LIMIT ?",
        (stream, cursor and cursor[0], cursor and cursor[0], limit),
    ).fetchall()
    if not rows:
        return 0
    with db:
        db.execute("INSERT INTO totals VALUES(?,?) ON CONFLICT(stream) DO UPDATE SET amount=amount+excluded.amount",
                   (stream, sum(row[2] for row in rows)))
    after_apply()
    with db:
        db.execute("INSERT OR REPLACE INTO progress VALUES(?,?,?)", (stream, *rows[-1][:2]))
    return len(rows)
