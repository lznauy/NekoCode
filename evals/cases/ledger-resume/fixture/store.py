import sqlite3

def connect(path):
    db = sqlite3.connect(path)
    db.executescript("""
        CREATE TABLE IF NOT EXISTS events(stream TEXT, ts INTEGER, id INTEGER, delta INTEGER, PRIMARY KEY(stream,ts,id));
        CREATE TABLE IF NOT EXISTS progress(stream TEXT PRIMARY KEY, ts INTEGER, id INTEGER);
        CREATE TABLE IF NOT EXISTS totals(stream TEXT PRIMARY KEY, amount INTEGER);
    """)
    return db
