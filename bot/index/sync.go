package index

import (
	"sync"

	indexerpkg "nekocode/bot/index/indexer"
	syncerpkg "nekocode/bot/index/syncer"
)

type Syncer = syncerpkg.Syncer

func NewSyncer(indexer *indexerpkg.Indexer, cwd string, graphMu *sync.RWMutex) (*Syncer, error) {
	return syncerpkg.NewSyncer(indexer, cwd, graphMu)
}
