package app

import (
	"fmt"
	"os"

	"nekocode/bot/app/sessionstate"
	"nekocode/bot/session"
)

func (b *Bot) saveSession() {
	if b.sess == nil {
		sess, err := session.New(b.cwd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "session: %v — skipping session persistence\n", err)
			return
		}
		b.sess = sess
	}
	snap := b.ctxMgr.Snapshot()
	b.mu.Lock()
	promptTokens, completionTokens := b.ag.TokenUsage()
	b.mu.Unlock()
	sessionstate.ApplyContextSnapshot(b.sess, snap, promptTokens, completionTokens, b.skillReg.LoadedSet())
	if err := b.sess.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "session: save error: %v\n", err)
	}
}

func (b *Bot) ResumeSession(id string) error {
	sess, err := session.Load(id)
	if err != nil {
		return fmt.Errorf("session: load: %w", err)
	}
	b.ctxMgr.Restore(sessionstate.ManagerSnapshot(sess))
	b.mu.Lock()
	b.ag.AddTokens(sess.PromptTokens, sess.CompletionTokens)
	b.mu.Unlock()
	for _, name := range sess.LoadedSkills {
		b.skillReg.MarkLoaded(name)
	}
	b.sess = sess
	return nil
}
