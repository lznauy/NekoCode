import json
from pathlib import Path


def load(path):
    raw = json.loads(Path(path).read_text())
    return raw["records"]


def save(path, records):
    Path(path).write_text(json.dumps({"version": 2, "records": records}, ensure_ascii=False))
