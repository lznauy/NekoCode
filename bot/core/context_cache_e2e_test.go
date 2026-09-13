package core

import (
	"context"
	"testing"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/provider/types"
)

func TestContextReportAfterResumeOfCompactedSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	newBot := func() *Bot {
		b := &Bot{cwd: t.TempDir(), ctxMgr: ctxmgr.New(ctxmgr.Config{
			Summarizer: func([]types.Message, string) (string, error) {
				return "<summary>durable summary of the earlier conversation</summary>", nil
			},
		})}
		b.initSession()
		return b
	}
	b := newBot()

	for range 8 {
		b.ctxMgr.Add("user", "old question")
		b.ctxMgr.Add("assistant", "old answer")
	}
	b.ctxMgr.RecordModelUsage(types.StreamUsage{
		PromptTokens: 1000, CacheHitTokens: 800, CacheMissTokens: 200, CacheUsageReported: true,
	})
	if err := b.saveSession(); err != nil {
		t.Fatal(err)
	}
	id := b.sess.CurrentID()

	if _, err := b.ctxMgr.Summarize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := b.saveSession(); err != nil {
		t.Fatal(err)
	}
	rC := b.ctxMgr.Report()
	t.Logf("after compaction: hit=%d miss=%d archive=%v compactCount=%d",
		rC.CacheHitTokens, rC.CacheMissTokens, rC.HasArchive, rC.CompactCount)

	b2 := newBot()
	if _, err := b2.resumeSession(id); err != nil {
		t.Fatal(err)
	}
	rR := b2.ctxMgr.Report()
	t.Logf("after resume of compacted: hit=%d miss=%d archive=%v compactCount=%d",
		rR.CacheHitTokens, rR.CacheMissTokens, rR.HasArchive, rR.CompactCount)
	if rR.CacheHitTokens != 800 || rR.CacheMissTokens != 200 {
		t.Fatalf("cache stats lost after resuming compacted session: hit=%d miss=%d", rR.CacheHitTokens, rR.CacheMissTokens)
	}
	if !rR.HasArchive || rR.CompactCount != 1 {
		t.Fatalf("archive state lost after resume: has=%v count=%d", rR.HasArchive, rR.CompactCount)
	}
}
