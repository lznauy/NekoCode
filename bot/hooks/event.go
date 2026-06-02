package hooks

import (
	"sync"
	"sync/atomic"
)

type Store struct {
	counters sync.Map // string → *atomic.Int64
	flags    sync.Map // string → *atomic.Bool
	gauges   sync.Map // string → *atomic.Int64
	values   sync.Map // string → *atomic.Value
	turns    sync.Map // string → *atomic.Int64
}

func (s *Store) getOrCreateCounter(k string) *atomic.Int64 {
	if v, ok := s.counters.Load(k); ok {
		return v.(*atomic.Int64)
	}
	n := new(atomic.Int64)
	actual, _ := s.counters.LoadOrStore(k, n)
	return actual.(*atomic.Int64)
}

func (s *Store) getOrCreateFlag(k string) *atomic.Bool {
	if v, ok := s.flags.Load(k); ok {
		return v.(*atomic.Bool)
	}
	b := new(atomic.Bool)
	actual, _ := s.flags.LoadOrStore(k, b)
	return actual.(*atomic.Bool)
}

func (s *Store) getOrCreateGauge(k string) *atomic.Int64 {
	if v, ok := s.gauges.Load(k); ok {
		return v.(*atomic.Int64)
	}
	n := new(atomic.Int64)
	actual, _ := s.gauges.LoadOrStore(k, n)
	return actual.(*atomic.Int64)
}

func (s *Store) getOrCreateValue(k string) *atomic.Value {
	if v, ok := s.values.Load(k); ok {
		return v.(*atomic.Value)
	}
	av := new(atomic.Value)
	av.Store("")
	actual, _ := s.values.LoadOrStore(k, av)
	return actual.(*atomic.Value)
}

func (s *Store) getOrCreateTurn(k string) *atomic.Int64 {
	if v, ok := s.turns.Load(k); ok {
		return v.(*atomic.Int64)
	}
	n := new(atomic.Int64)
	actual, _ := s.turns.LoadOrStore(k, n)
	return actual.(*atomic.Int64)
}

func (s *Store) IncCounter(k string)  { s.getOrCreateCounter(k).Add(1) }
func (s *Store) GetCounter(k string) int64 {
	if v, ok := s.counters.Load(k); ok { return v.(*atomic.Int64).Load() }
	return 0
}

func (s *Store) SetFlag(k string, v bool) { s.getOrCreateFlag(k).Store(v) }
func (s *Store) GetFlag(k string) bool {
	if v, ok := s.flags.Load(k); ok { return v.(*atomic.Bool).Load() }
	return false
}

func (s *Store) SetGauge(k string, v int64) { s.getOrCreateGauge(k).Store(v) }
func (s *Store) GetGauge(k string) int64 {
	if v, ok := s.gauges.Load(k); ok { return v.(*atomic.Int64).Load() }
	return 0
}

func (s *Store) SetValue(k string, v string) { s.getOrCreateValue(k).Store(v) }
func (s *Store) GetValue(k string) string {
	if v, ok := s.values.Load(k); ok { return v.(*atomic.Value).Load().(string) }
	return ""
}

func (s *Store) IncTurn(k string) { s.getOrCreateTurn(k).Add(1) }
func (s *Store) GetTurn(k string) int64 {
	if v, ok := s.turns.Load(k); ok { return v.(*atomic.Int64).Load() }
	return 0
}

func (s *Store) ResetTurn() {
	s.turns.Range(func(k, _ any) bool {
		s.turns.Delete(k)
		return true
	})
}

type Snapshot struct{ store *Store }

func (s *Snapshot) Counter(k string) int64  { return s.store.GetCounter(k) }
func (s *Snapshot) Flag(k string) bool       { return s.store.GetFlag(k) }
func (s *Snapshot) Gauge(k string) int64     { return s.store.GetGauge(k) }
func (s *Snapshot) Value(k string) string    { return s.store.GetValue(k) }
func (s *Snapshot) Turn(k string) int64      { return s.store.GetTurn(k) }
