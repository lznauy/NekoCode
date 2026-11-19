def billable_units(entries):
    return sum(entry["reserved"] for entry in entries)
