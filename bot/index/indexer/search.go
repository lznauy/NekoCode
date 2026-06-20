package indexer

// SearchFTS performs full-text symbol search through the backing index DB.
func (i *Indexer) SearchFTS(term string, limit int) ([]*Node, error) {
	return i.db.SearchFTS(term, limit)
}
