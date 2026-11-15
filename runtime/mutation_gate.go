package runtime

import "context"

// mutationGate is the single exclusion boundary for run admission, session and
// configuration changes, background scheduling and shutdown. New initializes it
// with capacity one. Waiting needs no timer or helper goroutine.
type mutationGate chan struct{}

func (g mutationGate) Lock() { g <- struct{}{} }
func (g mutationGate) Unlock() {
	select {
	case <-g:
	default:
		panic("runtime: unlock of unlocked mutation gate")
	}
}

func (g mutationGate) TryLock() bool {
	select {
	case g <- struct{}{}:
		return true
	default:
		return false
	}
}

func (g mutationGate) LockContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case g <- struct{}{}:
		// Cancellation and acquisition may become ready together. Do not let
		// a cancelled operation enter or retain the exclusion boundary.
		if err := ctx.Err(); err != nil {
			g.Unlock()
			return err
		}
		return nil
	}
}
