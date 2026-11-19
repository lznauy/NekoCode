import argparse
import json
from pathlib import Path

from replay import apply
from storage import load, save


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("snapshot")
    parser.add_argument("journal")
    args = parser.parse_args()
    records = load(args.snapshot)
    for line in Path(args.journal).read_text().splitlines():
        if line.strip():
            apply(records, json.loads(line))
            save(args.snapshot, records)


if __name__ == "__main__":
    main()
