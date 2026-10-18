package connect

import (
	"context"
	"time"

	controlruntime "nekocode/runtime"
)

// Capabilities declares which outbound affordances a channel can render.
// The dispatcher drops intents the channel cannot render, and channels use
// their own capabilities to pick a rendering (card vs plain text, keyboard
// vs slash commands).
type Capabilities struct {
	// EditMessages allows updating a sent message in place. It enables the
	// live preview (streaming deltas) and event-driven terminalization of
	// interactive messages. Channels without it never see preview intents —
	// multi-message "streaming" is spam, not a preview.
	EditMessages bool
	// Buttons allows attaching interactive actions to approval/question
	// messages. Without it, decision intents carry slash-command fallback
	// text instead.
	Buttons bool
	// RichCards allows structured interactive cards beyond plain text
	// (e.g. feishu interactive cards).
	RichCards bool
	// Images allows delivering image files referenced by outbound text as
	// native image messages. Without it the plain-text rendering carries
	// only the file paths — nothing is dropped, just not pictured.
	Images bool
}

// IntentKind classifies an outbound message by its semantics, not by how a
// channel happens to render it.
type IntentKind string

const (
	// IntentPreview is a fragment of in-progress assistant text. Delivered
	// only to channels with EditMessages.
	IntentPreview IntentKind = "preview"
	// IntentPreviewReset retracts the current model response from a preview.
	IntentPreviewReset IntentKind = "preview_reset"
	// IntentResult is the final output of a finished run.
	IntentResult IntentKind = "result"
	// IntentSystem is a command/system reply (e.g. /connect, /model) that
	// is not the output of a finished run. Kept distinct from IntentResult
	// so channels don't treat it as a run terminalization.
	IntentSystem IntentKind = "system"
	// IntentFailed reports a failed run.
	IntentFailed IntentKind = "failed"
	// IntentStopped reports a cancelled run.
	IntentStopped IntentKind = "stopped"
	// IntentApproval asks the user to decide a pending tool approval.
	IntentApproval IntentKind = "approval"
	// IntentQuestion asks the user to answer a pending question.
	IntentQuestion IntentKind = "question"
	// IntentApprovalResolved and IntentQuestionResolved report that a
	// pending decision was settled (possibly on another surface). They are
	// delivered to every channel: edit-capable channels terminalize the
	// live message, others may use them for bookkeeping (e.g. clearing a
	// pending-question tracker).
	IntentApprovalResolved IntentKind = "approval_resolved"
	IntentQuestionResolved IntentKind = "question_resolved"
)

// Canonical approval action IDs shared by every channel's buttons, card
// callbacks, and slash commands.
const (
	ActionOnce    = "once"
	ActionAlways  = "always"
	ActionReject  = "reject"
	ActionDismiss = "dismiss"
	ActionConfirm = "confirm"
)

// Action is a platform-agnostic decision affordance attached to an intent.
type Action struct {
	// ID is a canonical approval action ID, or an option index (decimal
	// string) for question options.
	ID    string
	Label string
}

// Intent is one platform-agnostic outbound message. Text is the canonical
// plain-text rendering and is always present; rich renderers may instead
// use the carried payloads (Approval/Question) to build cards or HTML.
type Intent struct {
	Kind    IntentKind
	RunID   controlruntime.RunID
	ID      string // approval/question ID for the interactive kinds
	Text    string
	Actions []Action
	// Approval is set for IntentApproval and IntentApprovalResolved.
	Approval *controlruntime.ApprovalView
	// Question is set for IntentQuestion and IntentQuestionResolved.
	Question *controlruntime.QuestionView
	// Verdict is the canonical outcome text for the resolved kinds
	// ("已批准", "已拒绝", "已回答", ...).
	Verdict string
	// Images lists local image file paths referenced by Text (run results
	// that mention generated images). Channels with the Images capability
	// deliver them as native image messages; others just render Text.
	Images []string
}

// Sink is the outbound contract a channel implements to plug into the
// shared event pipeline: declare capabilities, receive intents.
type Sink interface {
	Caps() Capabilities
	Post(ctx context.Context, in Intent) error
}

// Flusher is an optional Sink extension. Dispatch calls Flush on the
// returned interval so sinks can settle time-based state (e.g. preview
// edit throttling).
type Flusher interface {
	FlushInterval() time.Duration
	Flush(ctx context.Context)
}

// Tracker is an optional Sink extension: Dispatch feeds it every raw event
// synchronously, before translating it into intents. Sinks that keep
// per-run bookkeeping (e.g. telegram's run cards backing DoneReply) must
// use this hook rather than a parallel subscription — a second event
// subscription races the dispatch loop, and a result rendered from stale
// bookkeeping comes out empty (i.e. the message is silently lost).
type Tracker interface {
	Track(ev controlruntime.Event)
}

// Dispatch subscribes to the runtime broadcast and delivers translated
// intents to sink until ctx ends or the subscription closes. Intents the
// sink's capabilities cannot render are dropped here, so push policy lives
// in exactly one place (Translate) and capability gating in exactly one
// place (deliverable).
func Dispatch(ctx context.Context, rt controlruntime.ConnectorRuntime, sink Sink) error {
	return dispatch(ctx, rt, sink, nil)
}

// DispatchReady is Dispatch with a startup result. It reports nil only after
// the runtime event subscription exists, allowing connectors to avoid
// accepting an inbound task before its outbound path is ready.
func DispatchReady(ctx context.Context, rt controlruntime.ConnectorRuntime, sink Sink, ready chan<- error) error {
	return dispatch(ctx, rt, sink, ready)
}

func dispatch(ctx context.Context, rt controlruntime.ConnectorRuntime, sink Sink, ready chan<- error) error {
	events, err := rt.Events(ctx, controlruntime.EventFilter{})
	if err != nil {
		if ready != nil {
			ready <- err
		}
		return err
	}
	if ready != nil {
		ready <- nil
	}
	caps := sink.Caps()
	tracker, _ := sink.(Tracker)

	var tick <-chan time.Time
	var flusher Flusher
	if f, ok := sink.(Flusher); ok {
		if d := f.FlushInterval(); d > 0 {
			t := time.NewTicker(d)
			defer t.Stop()
			tick = t.C
			flusher = f
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick:
			flusher.Flush(ctx)
		case ev, ok := <-events:
			if !ok {
				return nil
			}
			if tracker != nil {
				tracker.Track(ev)
			}
			for _, in := range Translate(ev) {
				if !deliverable(in, caps) {
					continue
				}
				_ = sink.Post(ctx, in)
			}
		}
	}
}

// deliverable reports whether a channel with these capabilities can render
// the intent at all. Only the live preview is gated: without in-place
// edits, streaming degrades into a flood of standalone messages, which is
// strictly worse than receiving just the final result.
func deliverable(in Intent, caps Capabilities) bool {
	if in.Kind == IntentPreview || in.Kind == IntentPreviewReset {
		return caps.EditMessages
	}
	return true
}
