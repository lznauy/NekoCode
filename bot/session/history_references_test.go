package session

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

func seedSparseReferences(tb testing.TB, count int) *HistoryStore {
	tb.Helper()
	dir := tb.TempDir()
	f, err := os.Create(filepath.Join(dir, transcriptFileName))
	if err != nil {
		tb.Fatal(err)
	}
	enc := json.NewEncoder(f)
	for sequence := 1; sequence <= count; sequence++ {
		msg := types.Message{Role: "user", Content: "ordinary history without a reference"}
		switch sequence {
		case 1:
			msg = types.Message{Role: "tool", ToolCallID: "saved", ToolOutput: protocol.ToolOutputArtifact{Path: "/actor/result.txt"}}
		case count / 2:
			msg = types.Message{Role: "user", Source: types.MessageSourceSkillWorkflow, Content: "workflow", SkillDelivery: types.SkillDelivery{Schema: 1, Name: "review", Source: "selected", State: types.SkillReady, ContentHash: types.SkillContentHash("workflow"), OriginSequence: sequence}}
		}
		if err := enc.Encode(transcriptRecord{Seq: sequence, Message: msg}); err != nil {
			tb.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		tb.Fatal(err)
	}
	return newHistoryStore(dir)
}

func TestReferenceIndexSkipsSparseHistoryAndRespectsWatermark(t *testing.T) {
	store := seedSparseReferences(t, 10000)
	newest, err := store.ReferencePage(t.Context(), 0, 20, 64<<10, 10000)
	if err != nil || len(newest.Records) != 2 || newest.Records[0].Sequence != 5000 || newest.Records[1].Sequence != 1 || newest.HasMore || newest.Bytes > 2048 {
		t.Fatalf("sparse page=%+v err=%v", newest, err)
	}
	prefix, err := store.ReferencePage(t.Context(), 0, 20, 64<<10, 4999)
	if err != nil || len(prefix.Records) != 1 || prefix.Records[0].Sequence != 1 {
		t.Fatalf("read past watermark: %+v %v", prefix, err)
	}
	first, err := store.ReferencePage(t.Context(), 0, 1, 64<<10, 10000)
	if err != nil || len(first.Records) != 1 || !first.HasMore || first.Before != 5000 {
		t.Fatalf("wrong reference cursor: %+v %v", first, err)
	}
	if _, err := store.Append(10000, []types.Message{{Role: "user", Content: "new ordinary message"}, {Role: "tool", ToolCallID: "new", ToolOutput: protocol.ToolOutputArtifact{Path: "/actor/new.txt"}}}); err != nil {
		t.Fatal(err)
	}
	older, err := store.ReferencePage(t.Context(), first.Before, 20, 64<<10, 10002)
	if err != nil || len(older.Records) != 1 || older.Records[0].Sequence != 1 {
		t.Fatalf("append shifted old cursor: %+v %v", older, err)
	}
	cold := newHistoryStore(store.dir)
	latest, err := cold.ReferencePage(t.Context(), 0, 20, 64<<10, 10002)
	if err != nil || len(latest.Records) != 3 || latest.Records[0].Sequence != 10002 {
		t.Fatalf("cold lookup=%+v %v", latest, err)
	}
	probe, err := cold.ReferencePage(t.Context(), 0, 0, 1, 10002)
	if err != nil || !probe.HasMore || len(probe.Records) != 0 || probe.Bytes != 0 {
		t.Fatalf("probe loaded source bodies: %+v %v", probe, err)
	}
}

func TestReferenceIndexRebuildAndCorruptionDoNotRewriteSource(t *testing.T) {
	store := seedSparseReferences(t, 100)
	expected, err := store.ReferencePage(t.Context(), 0, 20, 64<<10, 100)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(store.dir, transcriptFileName)
	original, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(store.dir, "transcript.idx")
	v2, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	v1 := make([]byte, historyHeaderSize+100*32)
	copy(v1[:historyHeaderSize], v2[:historyHeaderSize])
	binary.LittleEndian.PutUint64(v1[:8], 0x314958484f4b454e)
	clear(v1[40:56])
	binary.LittleEndian.PutUint32(v1[56:60], crc32.ChecksumIEEE(v1[:56]))
	for i := range 100 {
		copy(v1[historyHeaderSize+i*32:historyHeaderSize+i*32+28], v2[historyHeaderSize+i*historyEntrySize:historyHeaderSize+i*historyEntrySize+28])
	}
	for _, legacy := range [][]byte{nil, v1, []byte("invalid cache")} {
		if legacy == nil {
			if err := os.Remove(index); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := os.WriteFile(index, legacy, 0600); err != nil {
				t.Fatal(err)
			}
		}
		got, err := newHistoryStore(store.dir).ReferencePage(t.Context(), 0, 20, 64<<10, 100)
		if err != nil || !reflect.DeepEqual(expected, got) {
			t.Fatalf("rebuild=%+v %v", got, err)
		}
	}
	f, err := os.OpenFile(index, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	// The terminal record's reference link is covered by the new entry CRC.
	if _, err := f.WriteAt([]byte{0xff}, historyHeaderSize+99*historyEntrySize+28); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReferencePage(t.Context(), 0, 20, 64<<10, 100); err == nil {
		t.Fatal("corrupt reference link accepted")
	}
	after, err := os.ReadFile(source)
	if err != nil || string(after) != string(original) {
		t.Fatal("reference lookup rewrote authoritative history")
	}
	if err := os.Remove(index); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.ReferencePage(canceled, 0, 20, 64<<10, 100); err == nil {
		t.Fatal("canceled lookup proceeded")
	}
	if _, err := os.Stat(index); !os.IsNotExist(err) {
		t.Fatal("canceled lookup started index rebuild")
	}
	if _, err := store.ReferencePage(t.Context(), 0, 20, 64<<10, 100); err != nil {
		t.Fatal(err)
	}
}

func TestReferencePageHonorsByteBudgetWithoutLosingCursor(t *testing.T) {
	store := seedSparseReferences(t, 100)
	first, err := store.ReferencePage(t.Context(), 0, 20, 1, 100)
	if err != nil || len(first.Records) != 1 || !first.HasMore {
		t.Fatalf("first indivisible record=%+v %v", first, err)
	}
	next, err := store.ReferencePage(t.Context(), first.Before, 20, 1, 100)
	if err != nil || len(next.Records) != 1 || next.Records[0].Sequence != 1 || next.HasMore {
		t.Fatalf("byte cap skipped reference=%+v %v", next, err)
	}
}

func BenchmarkReferencePageSparseHistory(b *testing.B) {
	for _, count := range []int{1000, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			store := seedSparseReferences(b, count)
			if _, err := store.Count(count, false); err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			for range b.N {
				if _, err := store.ReferencePage(context.Background(), 0, 20, 64<<10, count); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

type cancelReferenceRebuild struct {
	context.Context
	cancel context.CancelFunc
	checks int
}

func (c *cancelReferenceRebuild) Err() error {
	c.checks--
	if c.checks == 0 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestCanceledReferenceIndexRebuildResumesWithoutSourceRepair(t *testing.T) {
	store := seedSparseReferences(t, 1000)
	path := filepath.Join(store.dir, transcriptFileName)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	during := &cancelReferenceRebuild{Context: ctx, cancel: cancel, checks: 25}
	if _, err := store.ReferencePage(during, 0, 20, 64<<10, 1000); !errors.Is(err, context.Canceled) {
		t.Fatalf("rebuild ignored cancellation: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatal("canceled rebuild repaired or rewrote source")
	}
	rebuilt, err := newHistoryStore(store.dir).ReferencePage(t.Context(), 0, 20, 64<<10, 1000)
	if err != nil || len(rebuilt.Records) != 2 || rebuilt.Records[0].Sequence != 500 {
		t.Fatalf("interrupted cache could not rebuild: %+v %v", rebuilt, err)
	}
}
