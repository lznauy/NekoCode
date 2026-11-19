import json
from itertools import combinations
from pathlib import Path
import re
import sys

workspace, output = map(Path, sys.argv[1:])
path = workspace / "schedule.md"
lines = path.read_text(errors="replace").splitlines() if path.is_file() and path.resolve().is_relative_to(workspace) else []
table_lines = [i for i, line in enumerate(lines) if "|" in line]
rows = [[cell.strip() for cell in line.strip().strip("|").split("|")] for line in lines if "|" in line]
teams = set((Path(__file__).parent / "fixture/teams.txt").read_text().splitlines())
header = ["轮次", "一号场", "二号场", "三号场"]
structure = (len(rows) == 7 and table_lines[-1] - table_lines[0] == 6 and rows[0] == header and len(rows[1]) == 4 and
             all(re.fullmatch(r":?-+:?", c) for c in rows[1]) and
             all(len(row) == 4 and row[0] == str(i) for i, row in enumerate(rows[2:], 1)))
pairs, courts = [], set()
valid = structure
for row in rows[2:]:
    players = []
    for court, cell in enumerate(row[1:]):
        pair = re.split(r"\s+vs\s+", cell, flags=re.I)
        players.extend(pair)
        pairs.append(tuple(sorted(pair)))
        if "海盐" in pair:
            courts.add(court)
    valid = valid and len(players) == 6 and set(players) == teams
values = [("rounds", structure, "Five numbered rows and three named courts"),
          ("pairs", valid and set(pairs) == set(combinations(sorted(teams), 2)), f"{len(set(pairs) & set(combinations(sorted(teams), 2)))}/15 expected matchups present; each round covers all six teams: {valid}"),
          ("rotation", structure and len(courts) == 3, f"海盐 uses {len(courts)} courts")]
checks = [dict(id=k, status="passed" if ok else "failed", evidence=e) for k, ok, e in values]
(output / "result.json").write_text(json.dumps(dict(schema_version=1, checks=checks)))
