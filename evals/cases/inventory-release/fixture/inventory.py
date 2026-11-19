"""Inventory export prototype."""
import argparse
import csv

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--out", required=True)
    parser.add_argument("inputs", nargs="+")
    args = parser.parse_args()
    with open(args.out, "w") as output:
        output.write("sku,total\n")
        for filename in args.inputs:
            with open(filename) as source:
                for row in csv.DictReader(source):
                    output.write(f"{row['sku']},{row['delta']}\n")

if __name__ == "__main__":
    main()
