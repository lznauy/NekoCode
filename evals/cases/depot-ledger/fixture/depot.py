"""Version 1 inventory reader, retained for existing operators."""
import sqlite3
import sys

if __name__ == "__main__":
    db = sqlite3.connect(sys.argv[1])
    for site, sku, qty in db.execute("SELECT site,sku,qty FROM balances ORDER BY site,sku"):
        print(f"{site}\t{sku}\t{qty}")
