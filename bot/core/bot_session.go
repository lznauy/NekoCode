package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"nekocode/bot/checkpoint"
	"nekocode/bot/command"
	"nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool/runtime/execution"
	"nekocode/bot/policy/ledger"
	"nekocode/bot/provider/types"
	"nekocode/bot/session"
	"nekocode/protocol"
)

func (b *Bot) initSession() {
	b.sess = session.New(b.cwd)
	b.ctxMgr.SetSessionIDProvider(b.sess.CurrentID)
	b.ctxMgr.SetBeforeCompaction(func(compactionID string) error {
		if err := b.saveSession(); err != nil {
			return err
		}
		_, err := b.sess.BackupCurrent(compactionID)
		return err
	})
	// Compaction replaces the active history wholesale, so indexes captured
	// against it (selected-skill message ranges) must not survive the swap.
	b.ctxMgr.SetAfterCompaction(func() {
		if b.cmd != nil {
			b.cmd.ResetSkill()
		}
	})
	b.checkpoints = checkpoint.New("")
	b.checkpoints.Activate(b.sess.CurrentID(), nil, 0)
}

func (b *Bot) registerSessionCommands(p *command.Parser) {
	p.RegisterInfo("sessions", "Resume a saved session", func(ctx context.Context, cmd *command.Command) (string, bool) {
		if err := ctx.Err(); err != nil {
			return "Session command cancelled: " + err.Error(), true
		}
		if len(cmd.Args) == 0 {
			return formatSessionList(b.sess.List()), true
		}
		id := cmd.Args[0]
		snapshot, err := b.resumeSession(id)
		if err != nil {
			return fmt.Sprintf("Failed to resume session %s: %v", id, err), true
		}
		b.syncPolicySessionID()
		return fmt.Sprintf("Resumed session %s (%d messages restored).", id, len(snapshot.Transcript)), true
	})
	p.RegisterInfo("export", "Export conversation context", func(ctx context.Context, _ *command.Command) (string, bool) {
		if err := ctx.Err(); err != nil {
			return "Export cancelled: " + err.Error(), true
		}
		messages := b.ctxMgr.Snapshot().Transcript
		path, err := session.ExportMessages(messages, session.DefaultExportPath)
		if err != nil {
			return fmt.Sprintf("Failed to %v", err), true
		}
		return fmt.Sprintf("Context exported to %s (%d messages)", path, len(messages)), true
	})
	p.RegisterMenu("sessions", func(_ context.Context, cmd *command.Command) (protocol.CommandMenu, bool) {
		if len(cmd.Args) != 0 {
			return protocol.CommandMenu{}, false
		}
		current := b.sess.CurrentID()
		sessions := b.sess.List()
		items := make([]protocol.CommandMenuItem, 0, len(sessions))
		for _, item := range sessions {
			description := fmt.Sprintf("%s · %d messages · %s",
				time.Unix(item.CreatedAt, 0).Local().Format("01-02 15:04"), item.MsgCount, filepath.Base(item.CWD))
			items = append(items, protocol.CommandMenuItem{
				Key:   item.ID,
				Value: "/sessions " + item.ID, Label: item.ID,
				Description: description, Submit: true,
				Current: item.ID == current,
			})
		}
		return protocol.CommandMenu{Title: "Resume session", Empty: "No saved sessions", Items: items}, true
	})
}

func (b *Bot) saveSession() error {
	snapshot := b.sess.Current()
	if snapshot == nil {
		snapshot = b.sess.StartNew()
	}
	promptTokens, completionTokens := 0, 0
	if ag := b.getAgent(); ag != nil {
		promptTokens, completionTokens = ag.TokenUsage()
	}
	var loadedSkills map[string]bool
	if b.ext != nil {
		loadedSkills = b.ext.Snapshot().LoadedSkills
	}
	snapshot.CaptureContext(b.ctxMgr.Snapshot(), promptTokens, completionTokens, loadedSkills)
	if b.checkpoints != nil {
		snapshot.CheckpointTurns = b.checkpoints.Index(snapshot.ID)
		snapshot.CheckpointNext = b.checkpoints.Next(snapshot.ID)
	}
	snapshot.Ledger = b.ledgerSnapshot()
	err := b.sess.Save(snapshot)
	b.syncPolicySessionID()
	return err
}

func (b *Bot) CurrentSessionID() string { return b.sess.CurrentID() }

func (b *Bot) Conversation() contextmgr.ManagerSnapshot {
	if b.ctxMgr == nil {
		return contextmgr.ManagerSnapshot{}
	}
	return b.ctxMgr.Snapshot()
}

func (b *Bot) ResumeSession(id string) error {
	_, err := b.resumeSession(id)
	return err
}

func (b *Bot) resumeSession(id string) (*session.Snapshot, error) {
	oldID := b.sess.CurrentID()
	snapshot, err := b.sess.Load(id)
	if err != nil {
		return nil, err
	}
	if oldID != id {
		if err := b.closeSessionRuntime(oldID); err != nil {
			return nil, err
		}
	}
	if err := b.sess.Activate(snapshot); err != nil {
		return nil, err
	}
	b.ctxMgr.Restore(snapshot.ContextSnapshot())
	if b.checkpoints != nil {
		b.checkpoints.Activate(snapshot.ID, snapshot.CheckpointTurns, snapshot.CheckpointNext)
	}
	// Session files may contain a prompt from an older NekoCode version. Keep
	// conversation state, but always pair it with the current stable rules;
	// volatile environment data is injected separately on every Build.
	if b.promptBuilder != nil {
		b.ctxMgr.SetSystemPrompt(b.promptBuilder.BuildStatic())
	}
	if ag := b.getAgent(); ag != nil {
		ag.AddCompletionTokens(snapshot.CompletionTokens)
	}
	if b.ext != nil {
		b.ext.ClearLoadedSkills()
		for _, name := range snapshot.LoadedSkills {
			b.ext.MarkSkillLoaded(name)
		}
		// The list carries no per-session state, so re-rendering it here
		// reproduces the same bytes as before the reload (and only differs if
		// the registry itself changed) — the restored prefix stays cached.
		b.ext.RefreshSkillList()
	}
	b.restoreLedger(snapshot.Ledger)
	if b.cmd != nil {
		b.cmd.ResetSkill()
	}
	b.syncPolicySessionID()
	if err := b.recoverCheckpointEvents(snapshot.ID); err != nil {
		return nil, err
	}
	return snapshot, nil
}

func (b *Bot) ListSessions() []session.Meta {
	return b.sess.List()
}

func (b *Bot) NewSession() (*session.Snapshot, error) {
	_, err := b.resetConversation()
	if err != nil {
		return nil, err
	}
	return b.sess.Current(), nil
}

func (b *Bot) resetConversation() (string, error) {
	if err := b.closeSessionRuntime(b.sess.CurrentID()); err != nil {
		return "", err
	}
	b.ctxMgr.Reset()
	newSession := b.sess.StartNew()
	if b.checkpoints != nil {
		b.checkpoints.Activate(newSession.ID, nil, 0)
	}
	if b.ext != nil {
		b.ext.ClearLoadedSkills()
	}
	if b.cmd != nil {
		b.cmd.ResetSkill()
	}
	if b.policy != nil {
		b.policy.Restore(ledger.Snapshot{})
	}
	b.syncPolicySessionID()
	return "New session started.", nil
}

func (b *Bot) ensureSessionIdentity() {
	if b.sess == nil {
		return
	}
	if b.sess.CurrentID() == "" {
		newSession := b.sess.StartNew()
		if b.checkpoints != nil {
			b.checkpoints.Activate(newSession.ID, nil, 0)
		}
	}
	b.syncPolicySessionID()
}

func (b *Bot) DeleteSession(id string) error {
	if err := b.closeSessionRuntime(id); err != nil {
		return err
	}
	if err := b.sess.Delete(id); err != nil {
		return err
	}
	if b.checkpoints != nil {
		if err := b.checkpoints.Delete(id); err != nil {
			return err
		}
	}
	if b.sess.CurrentID() == id {
		b.ctxMgr.Reset()
		b.sess.ClearCurrent()
		if b.ext != nil {
			b.ext.ClearLoadedSkills()
		}
		if b.cmd != nil {
			b.cmd.ResetSkill()
		}
		if b.policy != nil {
			b.policy.Restore(ledger.Snapshot{})
		}
		b.syncPolicySessionID()
	}
	return nil
}

func (b *Bot) rewindCheckpoint(turn string) (string, error) {
	if b.checkpoints == nil || b.sess == nil {
		return "", fmt.Errorf("checkpoint rewind is unavailable")
	}
	result, err := b.checkpoints.Rewind(b.sess.CurrentID(), turn)
	if err != nil {
		var partial *checkpoint.PartialRewindError
		if errors.As(err, &partial) {
			b.invalidateRewindPaths(partial.Changes)
			event := formatPartialRewindEvent(result, err)
			b.ctxMgr.Add("user", event, types.MessageSourceRuntimeEvent)
		}
		return "", err
	}
	b.invalidateRewindPaths(result.Changes)
	event, directories := formatRewindEvent(result)
	b.ctxMgr.Add("user", event, types.MessageSourceRuntimeEvent)
	if err := b.saveSession(); err != nil {
		return "", fmt.Errorf("persist rewind event: %w", err)
	}
	if err := b.checkpoints.AcknowledgeRecovered(b.sess.CurrentID(), result.RewindID); err != nil {
		log.Printf("checkpoint: defer rewind journal cleanup: %v", err)
	}
	label := result.UserMessage
	if label == "" {
		label = "the selected message"
	}
	return fmt.Sprintf("Rewound to %q: %d files across %d directories.", label, len(result.Changes), len(directories)), nil
}

func (b *Bot) recoverCheckpointEvents(sessionID string) error {
	if b.checkpoints == nil {
		return nil
	}
	pending := b.checkpoints.Recovered(sessionID)
	added := false
	for _, result := range pending {
		if !b.hasRewindEvent(result.RewindID) {
			b.invalidateRewindPaths(result.Changes)
			event, _ := formatRewindEvent(result)
			b.ctxMgr.Add("user", event, types.MessageSourceRuntimeEvent)
			added = true
		}
	}
	if added {
		if err := b.saveSession(); err != nil {
			return fmt.Errorf("persist recovered rewind event: %w", err)
		}
	}
	for _, result := range pending {
		if err := b.checkpoints.AcknowledgeRecovered(sessionID, result.RewindID); err != nil {
			log.Printf("checkpoint: defer recovered rewind cleanup: %v", err)
		}
	}
	return nil
}

func (b *Bot) hasRewindEvent(rewindID string) bool {
	if rewindID == "" {
		return false
	}
	for _, message := range b.ctxMgr.Snapshot().Transcript {
		if message.Source == types.MessageSourceRuntimeEvent && strings.Contains(message.Content, rewindID) {
			return true
		}
	}
	return false
}

func (b *Bot) invalidateRewindPaths(changes []checkpoint.RollbackChange) {
	if ag := b.getAgent(); ag != nil {
		state := ag.ToolExecutionState()
		for _, change := range changes {
			state.FileCache.Invalidate(change.Path)
		}
		state.SnapshotStore = execution.NewSnapshotStore()
	}
}

func formatPartialRewindEvent(result checkpoint.Result, rewindErr error) string {
	payload := struct {
		Turn        string                      `json:"turn"`
		UserMessage string                      `json:"user_message,omitempty"`
		Files       []checkpoint.RollbackChange `json:"possibly_changed_files"`
		Error       string                      `json:"error"`
		Instruction string                      `json:"instruction"`
	}{
		Turn: result.Turn, UserMessage: result.UserMessage, Files: result.Changes, Error: rewindErr.Error(),
		Instruction: "The rewind rollback was incomplete. Treat every listed path as authoritative filesystem state and re-read it before further changes.",
	}
	encoded, _ := json.MarshalIndent(payload, "", "  ")
	return "<workspace_event type=\"checkpoint_rewind_partial\">\n" + string(encoded) + "\n</workspace_event>"
}

func formatRewindEvent(result checkpoint.Result) (string, []string) {
	directorySet := make(map[string]struct{}, len(result.Changes))
	for _, change := range result.Changes {
		directorySet[filepath.Dir(change.Path)] = struct{}{}
	}
	directories := make([]string, 0, len(directorySet))
	for directory := range directorySet {
		directories = append(directories, directory)
	}
	sort.Strings(directories)
	payload := struct {
		RewindID            string                      `json:"rewind_id"`
		Turn                string                      `json:"turn"`
		UserMessage         string                      `json:"user_message,omitempty"`
		Files               []checkpoint.RollbackChange `json:"files"`
		AffectedDirectories []string                    `json:"affected_directories"`
		Instruction         string                      `json:"instruction"`
		Scope               string                      `json:"scope"`
	}{
		RewindID: result.RewindID, Turn: result.Turn, UserMessage: result.UserMessage, Files: result.Changes, AffectedDirectories: directories,
		Instruction: "The filesystem is authoritative. Earlier tool results describing these paths are stale; re-read a file before making further changes.",
		Scope:       "Only the listed files were rolled back. Directory metadata was not restored; directories identify where affected files are located.",
	}
	encoded, _ := json.MarshalIndent(payload, "", "  ")
	return "<workspace_event type=\"checkpoint_rewind\">\n" + string(encoded) + "\n</workspace_event>", directories
}

func checkpointChangeCounts(turn checkpoint.TurnInfo) (created, modified, deleted int) {
	for _, change := range turn.Changes {
		switch change.Kind {
		case checkpoint.ChangeCreated:
			created++
		case checkpoint.ChangeModified:
			modified++
		case checkpoint.ChangeDeleted:
			deleted++
		}
	}
	return created, modified, deleted
}

func formatSessionList(sessions []session.Meta) string {
	if len(sessions) == 0 {
		return "No saved sessions."
	}
	var out strings.Builder
	out.WriteString("Saved sessions:\n")
	for _, item := range sessions {
		fmt.Fprintf(&out, "  %s  %s  %d msgs  %s\n", item.ID, item.Age(), item.MsgCount, item.CWD)
	}
	out.WriteString("\n/sessions <id> to resume")
	return out.String()
}

func (b *Bot) syncPolicySessionID() {
	if b.sess == nil {
		return
	}
	id := b.sess.CurrentID()
	if b.policy != nil {
		b.policy.SetSessionID(id)
	}
	if b.toolbox != nil {
		b.toolbox.SetSessionID(id)
	}
}

func (b *Bot) closeSessionRuntime(id string) error {
	if id == "" || b.toolbox == nil {
		return nil
	}
	return b.toolbox.CloseSession(id)
}

func (b *Bot) ledgerSnapshot() ledger.Snapshot {
	ag := b.getAgent()
	if ag == nil || ag.Governance() == nil {
		return ledger.Snapshot{}
	}
	return ag.Governance().Snapshot()
}

func (b *Bot) restoreLedger(snapshot ledger.Snapshot) {
	ag := b.getAgent()
	if ag != nil && ag.Governance() != nil {
		ag.Governance().Restore(snapshot)
	}
}
