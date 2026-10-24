package runstore

import (
	"fmt"
	"strings"
	"testing"

	"nekocode/runtime/internal/core"
)

func TestTextSnapshotsSurviveReplacementAndFurtherDeltas(t *testing.T) {
	s := NewRunStore(1)
	record := func(typ core.EventType, payload any) {
		s.Record(core.Event{RunID: "run_1", Type: typ, Payload: payload})
	}
	record(core.EventAssistantDelta, core.DeltaPayload{Delta: "preview"})
	record(core.EventReasoningDelta, core.DeltaPayload{Delta: "reason"})
	first, _ := s.Current()
	record(core.EventSystemMessage, core.MessagePayload{Content: "notice"})
	record(core.EventAssistantDelta, core.DeltaPayload{Delta: "more preview"})
	record(core.EventAssistantMessage, core.MessagePayload{Content: "answer"})
	second, _ := s.Current()
	if second.Output != "answer\nnotice" {
		t.Fatalf("canonical output = %q", second.Output)
	}
	record(core.EventAssistantDelta, core.DeltaPayload{Delta: "discard"})
	record(core.EventAssistantReset, nil)
	record(core.EventReasoningDelta, core.DeltaPayload{Delta: " more"})
	record(core.EventAssistantDelta, core.DeltaPayload{Delta: " tail"})
	third, _ := s.Current()
	if third.Output != "answer\nnotice tail" || third.Reasoning != "reason more" {
		t.Fatalf("live snapshot = %+v", third)
	}
	record(core.EventRunDone, core.RunResult{Output: "final"})
	final, _ := s.Current()
	if final.Output != "final" {
		t.Fatalf("final output = %q", final.Output)
	}
	if first.Output != "preview" || first.Reasoning != "reason" || second.Output != "answer\nnotice" || third.Output != "answer\nnotice tail" {
		t.Fatal("previous snapshots were mutated")
	}
}

func BenchmarkStreamText(b *testing.B) {
	for _, n := range []int{1024, 4096, 16384} {
		b.Run(fmt.Sprintf("%d_chunks", n), func(b *testing.B) {
			ev := core.Event{RunID: "run_1", Type: core.EventAssistantDelta, Payload: core.DeltaPayload{Delta: strings.Repeat("x", 16)}}
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				s := NewRunStore(1)
				for j := 0; j < n; j++ {
					s.Record(ev)
				}
				snap, _ := s.Current()
				if len(snap.Output) != 16*n {
					b.Fatal("incomplete output")
				}
			}
		})
	}
}

func TestCanonicalOutputSurvivesLaterAppendAndReset(t *testing.T) {
	store := NewRunStore(1)
	record := func(typ core.EventType, payload any) {
		store.Record(core.Event{RunID: "run_1", Type: typ, Payload: payload})
	}
	record(core.EventRunDone, core.RunResult{Output: "canonical"})
	before, _ := store.Current()
	record(core.EventSystemMessage, core.MessagePayload{Content: "notice"})
	record(core.EventAssistantDelta, core.DeltaPayload{Delta: "discard"})
	record(core.EventAssistantReset, nil)
	after, _ := store.Current()
	if before.Output != "canonical" || after.Output != "canonical\nnotice" {
		t.Fatalf("canonical output was overwritten: before=%q after=%q", before.Output, after.Output)
	}
	record(core.EventAssistantMessage, core.MessagePayload{Content: " more"})
	last, _ := store.Current()
	if last.Output != "canonical\nnotice more" || after.Output != "canonical\nnotice" {
		t.Fatal("append mutated an earlier snapshot")
	}
}

func BenchmarkCanonicalOutput(b *testing.B) {
	ev := core.Event{RunID: "run_1", Type: core.EventRunDone, Payload: core.RunResult{Output: strings.Repeat("x", 64*1024)}}
	store := NewRunStore(1)
	b.ReportAllocs()
	b.ResetTimer()
	for j := 0; j < b.N; j++ {
		store.Record(ev)
	}
}
