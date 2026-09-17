package headless

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
)

type readResult struct {
	frame frame
	err   error
}
type writeItem struct {
	data    []byte
	barrier chan struct{}
}

// One reader and one writer keep reverse calls responsive while a turn runs.
// Queue bounds fail the connection explicitly; they never discard frames.
type transport struct {
	ctx         context.Context
	reads       chan readResult
	writes      chan writeItem
	errors      chan error
	mu          sync.Mutex
	queuedBytes int
	wg          sync.WaitGroup
}

func newTransport(ctx context.Context, in io.Reader, out io.Writer) *transport {
	t := &transport{ctx: ctx, reads: make(chan readResult), writes: make(chan writeItem, 128), errors: make(chan error, 1)}
	t.wg.Add(2)
	go func() {
		defer t.wg.Done()
		scanner := bufio.NewScanner(in)
		scanner.Buffer(make([]byte, 64*1024), maxFrameSize)
		for scanner.Scan() {
			var f frame
			err := json.Unmarshal(scanner.Bytes(), &f)
			if err != nil || f.Type == "" {
				err = errors.New("invalid JSON frame or missing type")
			}
			select {
			case t.reads <- readResult{frame: f, err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
		err := scanner.Err()
		if err == nil {
			err = io.EOF
		}
		select {
		case t.reads <- readResult{err: err}:
		case <-ctx.Done():
		}
	}()
	go func() {
		defer t.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case item := <-t.writes:
				if item.barrier != nil {
					close(item.barrier)
					continue
				}
				n, err := out.Write(item.data)
				if err == nil && n != len(item.data) {
					err = io.ErrShortWrite
				}
				t.mu.Lock()
				t.queuedBytes -= len(item.data)
				t.mu.Unlock()
				if err != nil {
					t.errors <- err
					return
				}
			}
		}
	}()
	return t
}

func (t *transport) send(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return t.sendEncoded(data)
}

// sendEncoded takes ownership of an already encoded frame. It shares the same
// size, backpressure and cancellation checks as send.
func (t *transport) sendEncoded(data []byte) error {
	if len(data) >= maxFrameSize {
		return errors.New("output frame exceeds 8 MiB")
	}
	data = append(data, '\n')
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.queuedBytes+len(data) > 16<<20 {
		return errors.New("output byte queue overflow")
	}
	select {
	case <-t.ctx.Done():
		return t.ctx.Err()
	case err := <-t.errors:
		return err
	default:
	}
	select {
	case t.writes <- writeItem{data: data}:
		t.queuedBytes += len(data)
		return nil
	default:
		return errors.New("output frame queue overflow")
	}
}

func (t *transport) flush(ctx context.Context) error {
	done := make(chan struct{})
	select {
	case t.writes <- writeItem{barrier: done}:
	case err := <-t.errors:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-done:
		return nil
	case err := <-t.errors:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
