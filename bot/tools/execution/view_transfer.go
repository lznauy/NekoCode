package execution

func (s *ViewStore) Seed(src *ViewStore) {
	s.transferFrom(src, false)
}

func (s *ViewStore) Merge(other *ViewStore) {
	s.transferFrom(other, true)
}

func (s *ViewStore) transferFrom(src *ViewStore, overwrite bool) {
	if s == nil || src == nil {
		return
	}
	src.mu.RLock()
	s.mu.Lock()
	defer s.mu.Unlock()
	defer src.mu.RUnlock()

	for id, e := range src.entries {
		if _, exists := s.entries[id]; exists && !overwrite {
			continue
		}
		cp := e
		if e.LineHashes != nil {
			cp.LineHashes = make(map[int]string, len(e.LineHashes))
			for k, v := range e.LineHashes {
				cp.LineHashes[k] = v
			}
		}
		if e.Lines != nil {
			cp.Lines = make(map[int]string, len(e.Lines))
			for k, v := range e.Lines {
				cp.Lines[k] = v
			}
		}
		if _, exists := s.entries[id]; !exists {
			s.order = append(s.order, id)
		}
		s.entries[id] = cp
	}
	s.evictIfNeeded()
}
