package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/provider/types"
	"nekocode/util/attachment"
)

func BenchmarkListSubagents(b *testing.B) {
	for _, count := range []int{100, 1000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			b.Setenv("HOME", b.TempDir())
			root, err := subagentRoot("parent")
			if err != nil {
				b.Fatal(err)
			}
			for i := 0; i < count; i++ {
				s := newSnapshot("/workspace")
				s.ID = fmt.Sprintf("task-%04d", i)
				s.Subagent = &SubagentMetadata{ParentSessionID: "parent", Profile: "explore", Status: "completed", Prompt: "inspect", Result: "done"}
				d := filepath.Join(root, s.ID)
				if err := os.MkdirAll(d, 0700); err != nil {
					b.Fatal(err)
				}
				data, err := json.Marshal(s)
				if err != nil {
					b.Fatal(err)
				}
				path := filepath.Join(d, "session.json")
				if err := os.WriteFile(path, data, 0600); err != nil {
					b.Fatal(err)
				}
				stat, err := os.Stat(path)
				if err != nil {
					b.Fatal(err)
				}
				data, err = json.Marshal(subagentProjection{SourceSize: stat.Size(), SourceTime: stat.ModTime().UnixNano(), Info: infoFromSnapshot(s)})
				if err != nil {
					b.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(d, "observation.json"), data, 0600); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				items, err := ListSubagents("parent")
				if err != nil || len(items) != count {
					b.Fatalf("items=%d err=%v", len(items), err)
				}
			}
		})
	}
}

func BenchmarkForkHistory(b *testing.B) {
	for _, count := range []int{1000, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			b.Setenv("HOME", b.TempDir())
			m := New(b.TempDir())
			s := m.Current()
			content := strings.Repeat("history content ", 64)
			for i := 0; i < count; i++ {
				s.Transcript = append(s.Transcript, types.Message{Role: "user", Content: content})
			}
			s.Messages = cloneMessages(s.Transcript[count-10:])
			if err := m.Save(s); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				fork, err := m.Fork(b.Context(), s.ID)
				if err != nil || len(fork.Transcript) != count {
					b.Fatalf("fork failed: %v", err)
				}
				b.StopTimer()
				if err := m.Delete(fork.ID); err != nil {
					b.Fatal(err)
				}
				if err := attachment.DeleteSession(fork.ID); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
			}
		})
	}
}
