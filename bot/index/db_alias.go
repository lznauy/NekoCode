package index

import dbpkg "nekocode/bot/index/db"

type DB = dbpkg.DB

func OpenDB(path string) (*DB, error) {
	return dbpkg.OpenDB(path)
}
