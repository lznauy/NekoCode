package hooks

type StatePatch struct {
	Ints    map[string]int64
	Strings map[string]string
}

func (p *StatePatch) setInt(k string, v int64) {
	if p.Ints == nil {
		p.Ints = make(map[string]int64)
	}
	p.Ints[k] = v
}

func (p *StatePatch) setString(k, v string) {
	if p.Strings == nil {
		p.Strings = make(map[string]string)
	}
	p.Strings[k] = v
}

type Snapshot struct {
	Store   map[string]int64
	Tool    string
	Args    map[string]any
	Error   bool
	strVals map[string]string
	patch   StatePatch
}

func (s *Snapshot) get(k string) int64     { return s.Store[k] }
func (s *Snapshot) flag(k string) bool     { return s.Store[k] == 1 }
func (s *Snapshot) getStr(k string) string { return s.strVals[k] }

func (s *Snapshot) set(k string, v int64) {
	s.Store[k] = v
	s.patch.setInt(k, v)
}

func (s *Snapshot) setStr(k, v string) {
	s.strVals[k] = v
	s.patch.setString(k, v)
}
