package session

import (
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"
	"time"
)

func TestHistoryScalingMeasurements(t *testing.T) {
	if os.Getenv("NEKOCODE_HISTORY_MEASURE") != "1" {
		t.Skip("explicit measurement")
	}
	for _, n := range []int{10000, 100000} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			s := seedIndexedHistory(t, n)
			measureHistoryStore(t, s, n)
		})
	}
	if source := os.Getenv("NEKOCODE_HISTORY_FIXTURE"); source != "" {
		t.Run("real_copy", func(t *testing.T) {
			data, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			if err = os.WriteFile(filepath.Join(dir, transcriptFileName), data, 0600); err != nil {
				t.Fatal(err)
			}
			data = nil
			measureHistoryStore(t, newHistoryStore(dir), 5524)
		})
	}
}
func measureHistoryStore(t *testing.T, s *HistoryStore, n int) {
	start := time.Now()
	count, err := s.Count(n, true)
	if err != nil {
		t.Fatal(err)
	}
	cold := time.Since(start)
	goruntime.GC()
	var before, after goruntime.MemStats
	goruntime.ReadMemStats(&before)
	start = time.Now()
	p, err := s.Page(0, 100, HistoryPageBytes, n)
	if err != nil {
		t.Fatal(err)
	}
	warm := time.Since(start)
	goruntime.GC()
	goruntime.ReadMemStats(&after)
	goruntime.KeepAlive(p)
	t.Logf("records=%d cold_index=%s warm_page=%s page_records=%d retained_heap_delta=%d", count, cold, warm, len(p.Messages), int64(after.HeapAlloc)-int64(before.HeapAlloc))
}
