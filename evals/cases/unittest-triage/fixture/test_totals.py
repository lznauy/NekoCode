import unittest

from totals import billable_units


class Usage(unittest.TestCase):
    def test_charge_completed_units(self):
        entries = [{"reserved": 9, "completed": 4}, {"reserved": 3, "completed": 2}]
        self.assertEqual(billable_units(entries), 6)
