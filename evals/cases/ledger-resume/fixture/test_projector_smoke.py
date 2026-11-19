import unittest

from projector import advance
from store import connect


class HealthyBatch(unittest.TestCase):
    def test_different_timestamps(self):
        db = connect(":memory:")
        self.addCleanup(db.close)
        with db:
            db.executemany("INSERT INTO events VALUES(?,?,?,?)", [("north", 4, 1, 5), ("north", 6, 2, -3)])
        self.assertEqual(advance(db, "north", 20), 2)
        self.assertEqual(advance(db, "north", 20), 0)
        self.assertEqual(db.execute("SELECT amount FROM totals WHERE stream='north'").fetchone(), (2,))
