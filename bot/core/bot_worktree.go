package core

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"nekocode/bot/checkpoint"
	"nekocode/bot/command"
	"nekocode/bot/worktree"
)

type worktreeApplyCheckpoint struct {
	manager *checkpoint.Manager
	session string
	message string
	begun   bool
}

func (c *worktreeApplyCheckpoint) Begin() error {
	_, err := c.manager.BeginMessage(c.session, c.message)
	c.begun = err == nil
	return err
}
func (c *worktreeApplyCheckpoint) Capture(path string) error  { return c.manager.Capture(path) }
func (c *worktreeApplyCheckpoint) Finalize(path string) error { return c.manager.Finalize(path) }

func (b *Bot) registerWorktreeCommands(p *command.Parser) {
	p.RegisterInfo("worktree", "Manage isolated task results: list, diff, apply, remove", func(ctx context.Context, cmd *command.Command) (string, bool) {
		usage := "Usage: /worktree list | diff <task-id> | apply <task-id> | remove <task-id> [--discard]"
		args := cmd.Args
		if len(args) == 0 || len(args) == 1 && args[0] == "list" {
			items, err := worktree.List(b.sess.CurrentID())
			if err != nil {
				return err.Error(), true
			}
			if len(items) == 0 {
				return "No retained worktrees in this session.", true
			}
			var out strings.Builder
			for _, i := range items {
				fmt.Fprintf(&out, "%s  %s  base=%s\n", i.TaskID, i.Path, i.Base)
			}
			return out.String(), true
		}
		if len(args) < 2 || len(args) > 3 {
			return usage, true
		}
		action, id := args[0], args[1]
		discard := len(args) == 3 && args[2] == "--discard"
		if action != "diff" && action != "apply" && action != "remove" || len(args) == 3 && (!discard || action != "remove") {
			return usage, true
		}
		lease, err := worktree.Acquire(b.sess.CurrentID(), id)
		if err != nil {
			return err.Error(), true
		}
		defer lease.Close()
		if discard {
			_, err = lease.LoadForRemoval(ctx, b.cwd, id)
		} else {
			_, err = lease.Load(ctx, b.cwd, id)
		}
		if err != nil {
			return err.Error(), true
		}
		switch action {
		case "diff":
			patch, err := lease.Diff(ctx)
			if err != nil {
				return err.Error(), true
			}
			if len(patch) == 0 {
				return "No changes relative to the task base.", true
			}
			return string(patch), true
		case "apply":
			sessionID := b.sess.CurrentID()
			var cp *worktreeApplyCheckpoint
			var hooks []worktree.ApplyCheckpoint
			if b.checkpoints != nil {
				cp = &worktreeApplyCheckpoint{manager: b.checkpoints, session: sessionID, message: "/worktree apply " + id}
				hooks = append(hooks, cp)
			}
			changed, err := lease.Apply(ctx, hooks...)
			if cp != nil && cp.begun {
				err = errors.Join(err, b.checkpoints.Finish(sessionID))
			}
			if err != nil {
				if changed {
					return "Worktree changes were applied, but checkpoint finalization failed: " + err.Error(), true
				}
				return "Worktree apply failed; result retained: " + err.Error(), true
			}
			if !changed {
				return "No changes to apply.", true
			}
			message := "Applied worktree changes to the main working copy. Review and test them there. Worktree retained until explicitly removed."
			if cp != nil && cp.begun {
				message += " Use /rewind to restore the pre-apply files."
			}
			return message, true
		case "remove":
			if err = lease.Remove(ctx, discard); err != nil {
				return err.Error(), true
			}
			return "Worktree removed. Task history retained; this task can no longer resume.", true
		}
		return usage, true
	})
}
