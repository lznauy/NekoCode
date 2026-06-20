package index

import indexerpkg "nekocode/bot/index/indexer"

type Indexer = indexerpkg.Indexer

func NewIndexer(dbPath string) (*Indexer, error) {
	return indexerpkg.NewIndexer(dbPath)
}

func ShouldSkipDir(name string) bool {
	return indexerpkg.ShouldSkipDir(name)
}

func SupportsFile(path string) bool {
	return indexerpkg.SupportsFile(path)
}
