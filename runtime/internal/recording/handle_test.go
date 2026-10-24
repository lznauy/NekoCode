package recording

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"nekocode/runtime/internal/core"
)

func TestRecorderReusesAndReleasesLog(t *testing.T) {
	r, err := NewEventRecorder(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ev := core.Event{RunID: "run_1", Type: core.EventAssistantDelta, Time: time.Now(), Payload: core.DeltaPayload{Delta: "hello"}}
	if err := r.RecordError(ev); err != nil {
		t.Fatal(err)
	}
	run := r.runs[ev.RunID]
	first := run.events
	if err := r.RecordError(ev); err != nil {
		t.Fatal(err)
	}
	if run.events != first {
		t.Fatal("log descriptor replaced between events")
	}
	if err := r.BindSession("bound-session"); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Write(nil); err == nil {
		t.Fatal("old descriptor still open after binding")
	}
	if err := r.RecordError(ev); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(run.dir, "events.jsonl")
	last := run.events
	r.EndRun(ev.RunID)
	if _, err := last.Write(nil); err == nil {
		t.Fatal("descriptor still open after EndRun")
	}
	events, err := loadRecordedEventFile(path)
	if err != nil || len(events) != 3 {
		t.Fatalf("events=%d err=%v", len(events), err)
	}
	ev.RunID = "run_2"
	if err := r.RecordError(ev); err != nil {
		t.Fatal(err)
	}
	last = r.runs[ev.RunID].events
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := last.Write(nil); err == nil {
		t.Fatal("descriptor still open after Close")
	}
}

func BenchmarkLogAppend(b *testing.B) {
	data := []byte("{\"type\":\"assistant_delta\",\"payload\":{\"delta\":\"hello world\"}}\n")
	for _, reuse := range []bool{false, true} {
		name := "open_each_event"
		if reuse {
			name = "reuse_handle"
		}
		b.Run(name, func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "events.jsonl")
			var f *os.File
			var err error
			if reuse {
				f, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
				if err != nil {
					b.Fatal(err)
				}
				defer f.Close()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if !reuse {
					if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
						b.Fatal(err)
					}
					f, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
					if err != nil {
						b.Fatal(err)
					}
				}
				if _, err = f.Write(data); err != nil {
					b.Fatal(err)
				}
				if !reuse {
					if err = f.Close(); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}
