package interaction

import (
	"context"
	"fmt"
	"time"

	controlruntime "nekocode/runtime"
)

type eventRuntime interface {
	controlruntime.RunEvents
	CancelRun(context.Context, controlruntime.RunID) error
}

type runSnapshotReader interface {
	CurrentRun() (controlruntime.RunSnapshot, bool)
}

// SubscribeRunEvents keeps interactive surfaces subscribed after an overflow.
// Lost controls cannot safely be replayed: reconnect first, cancel the affected
// run, and show a failure so the user can retry with a working event stream.
func SubscribeRunEvents(ctx context.Context, rt eventRuntime, filter controlruntime.EventFilter) (<-chan controlruntime.Event, error) {
	streamCtx, cancel := context.WithCancel(ctx)
	// Establish the baseline before subscribing so runs completed during a
	// later reconnect can be distinguished from preexisting history.
	var initialFinishedRun controlruntime.RunID
	reader, canRead := rt.(runSnapshotReader)
	if canRead {
		if run, exists := reader.CurrentRun(); exists {
			if _, terminal := terminalEventType(run.Status); terminal {
				initialFinishedRun = run.ID
			}
		}
	}
	events, err := rt.Events(streamCtx, filter)
	if err != nil {
		cancel()
		return nil, err
	}
	if events == nil {
		cancel()
		return nil, nil
	}
	out := make(chan controlruntime.Event)
	go func() {
		defer cancel()
		defer close(out)
		var lastRun controlruntime.RunID
		var finishedRun controlruntime.RunID
		var interrupted controlruntime.RunID
		send := func(ev controlruntime.Event) bool {
			select {
			case out <- ev:
				return true
			case <-streamCtx.Done():
				return false
			}
		}
		for {
			select {
			case <-streamCtx.Done():
				return
			case ev, ok := <-events:
				if !ok {
					if streamCtx.Err() != nil {
						return
					}
					// Also bounds retries for implementations that repeatedly
					// return an already-closed channel.
					timer := time.NewTimer(100 * time.Millisecond)
					select {
					case <-timer.C:
					case <-streamCtx.Done():
						timer.Stop()
						return
					}
					next, err := rt.Events(streamCtx, filter)
					if err != nil || next == nil {
						return
					} // Runtime shutdown closes subscriptions.
					events = next
					// Read after subscribing: a task may finish in the reconnect
					// gap. Its snapshot supplies the terminal event we missed.
					affected := lastRun
					var recovered *controlruntime.Event
					if canRead {
						if run, exists := reader.CurrentRun(); exists && run.ID != "" {
							affected = run.ID
							if typ, terminal := terminalEventType(run.Status); terminal {
								if run.ID == initialFinishedRun && lastRun != run.ID {
									affected = ""
								} else {
									recovered = &controlruntime.Event{
										Type: typ, RunID: run.ID, Source: controlruntime.SourceRef{Kind: "runtime"},
										Payload: controlruntime.RunResult{Output: run.Output, Error: run.Error},
									}
								}
							}
						}
					}
					if affected == finishedRun || affected == interrupted {
						affected = ""
					}
					if affected == "" {
						continue
					}
					if recovered != nil {
						finishedRun, interrupted, lastRun = affected, affected, ""
						if !send(*recovered) {
							return
						}
						continue
					}
					stopCtx, stop := context.WithTimeout(streamCtx, time.Second)
					cancelErr := rt.CancelRun(stopCtx, affected)
					stop()
					message := "Runtime event stream was interrupted. Please retry the affected task."
					if cancelErr != nil {
						message = fmt.Sprintf("Runtime event stream was interrupted; cancellation failed: %v", cancelErr)
					}
					// Settle once even if cancellation times out. Queued controls
					// from this run must not reopen an already-failed interaction.
					finishedRun, interrupted, lastRun = affected, affected, ""
					if !send(controlruntime.Event{
						Type: controlruntime.EventRunFailed, RunID: affected,
						Source:  controlruntime.SourceRef{Kind: "runtime"},
						Payload: controlruntime.RunResult{Error: message},
					}) {
						return
					}
					continue
				}
				if interrupted != "" && ev.RunID == interrupted {
					continue // The recovered run is already settled, including its controls.
				}
				if ev.RunID != "" && ev.RunID != finishedRun {
					lastRun = ev.RunID
				}
				if ev.RunID != "" && (ev.Type == controlruntime.EventRunDone || ev.Type == controlruntime.EventRunFailed || ev.Type == controlruntime.EventRunCancelled) {
					finishedRun = ev.RunID
					if lastRun == ev.RunID {
						lastRun = ""
					}
				}
				if !send(ev) {
					return
				}
			}
		}
	}()
	return out, nil
}

func terminalEventType(status controlruntime.RunStatus) (controlruntime.EventType, bool) {
	switch status {
	case controlruntime.RunDone:
		return controlruntime.EventRunDone, true
	case controlruntime.RunFailed:
		return controlruntime.EventRunFailed, true
	case controlruntime.RunCancelled:
		return controlruntime.EventRunCancelled, true
	default:
		return "", false
	}
}
