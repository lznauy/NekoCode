package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"nekocode/util/attachment"
)

const maxMessageSize = 1 << 20

// Include one full-size image in base64 plus the normal message envelope.
// Multiple image blocks share the same frame budget.
const maxPromptFrameSize = (attachment.MaxImageBytes+2)/3*4 + maxMessageSize
const maxInflightRequestBytes = 64 << 20

var errFrameTooLarge = errors.New("ACP prompt frame exceeds size limit")

const maxConcurrentRequests = 64
const maxQueuedWrites = 128

type handler func(context.Context, string, json.RawMessage) (any, *wireError)

type outboundMessage struct {
	ctx  context.Context
	body []byte
	done chan error
}

type connection struct {
	out io.Writer

	writeMu  sync.Mutex
	writeQ   chan outboundMessage
	stopQ    chan struct{}
	ctxDone  <-chan struct{}
	cancel   context.CancelFunc
	writeErr chan error
	mu       sync.Mutex
	pending  map[string]chan message
	nextID   atomic.Uint64
}

func newConnection(out io.Writer) *connection {
	return &connection{out: out, pending: make(map[string]chan message)}
}

// serve owns cancellation of closable inputs: Close must interrupt Read.
// Nonclosable readers must be finite or provide their own cancellation.
func (c *connection) serve(ctx context.Context, in io.Reader, handle handler) error {
	parent := ctx
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if closer, ok := in.(io.Closer); ok {
		closed := make(chan struct{})
		stop := context.AfterFunc(ctx, func() { _ = closer.Close(); close(closed) })
		defer func() {
			if !stop() {
				<-closed
			}
		}()
	}
	c.writeQ = make(chan outboundMessage, maxQueuedWrites)
	c.stopQ = make(chan struct{})
	c.writeErr = make(chan error, 1)
	c.ctxDone = ctx.Done()
	c.cancel = cancel
	go c.writeLoop()
	defer close(c.stopQ)

	reader := bufio.NewReaderSize(in, 64*1024)
	var readErr error
	var inflightBytes atomic.Int64
	var workers sync.WaitGroup
	slots := make(chan struct{}, maxConcurrentRequests)
	cancelSlot := make(chan struct{}, 1)
readLoop:
	for ctx.Err() == nil {
		line, err := readFrame(reader, maxPromptFrameSize)
		if errors.Is(err, errFrameTooLarge) {
			if !c.tryWrite(map[string]any{"jsonrpc": "2.0", "id": nil, "error": rpcError(-32600, "%s", err)}) {
				break
			}
			continue
		}
		if err != nil {
			readErr = err
			break
		}
		if ctx.Err() != nil {
			break
		}
		var msg message
		if err := json.Unmarshal(line, &msg); err != nil {
			if !c.tryWrite(map[string]any{"jsonrpc": "2.0", "id": nil, "error": rpcError(-32700, "parse error")}) {
				break
			}
			continue
		}
		if msg.JSONRPC != "2.0" {
			if !c.tryWrite(map[string]any{"jsonrpc": "2.0", "id": rawID(msg.ID), "error": rpcError(-32600, "invalid request")}) {
				break
			}
			continue
		}
		if len(line) > maxMessageSize && msg.Method != "session/prompt" {
			failure := rpcError(-32600, "message exceeds 1 MiB")
			if msg.Method == "" {
				c.resolve(message{ID: msg.ID, Error: failure})
				continue
			}
			if len(msg.ID) != 0 && string(msg.ID) != "null" && !c.tryWrite(map[string]any{"jsonrpc": "2.0", "id": rawID(msg.ID), "error": failure}) {
				break
			}
			continue
		}
		if msg.Method == "" {
			c.resolve(msg)
			continue
		}
		slot := slots
		if msg.Method == "session/cancel" {
			slot = cancelSlot
		}
		select {
		case slot <- struct{}{}:
		default:
			if len(msg.ID) != 0 && string(msg.ID) != "null" {
				if !c.tryWrite(map[string]any{
					"jsonrpc": "2.0", "id": rawID(msg.ID),
					"error": rpcError(-32000, "too many concurrent requests"),
				}) {
					// A plain break here would only exit the select and the
					// request would still be dispatched — without a slot, its
					// release would block forever. Terminate the read loop.
					break readLoop
				}
			}
			continue
		}
		size := int64(len(line))
		if msg.Method != "session/cancel" && inflightBytes.Load()+size > maxInflightRequestBytes {
			<-slot
			if len(msg.ID) != 0 && string(msg.ID) != "null" && !c.tryWrite(map[string]any{
				"jsonrpc": "2.0", "id": rawID(msg.ID), "error": rpcError(-32000, "in-flight request byte limit reached"),
			}) {
				break
			}
			continue
		}
		inflightBytes.Add(size)
		workers.Add(1)
		go func(msg message, slot chan struct{}) {
			defer workers.Done()
			defer inflightBytes.Add(-size)
			defer func() { <-slot }()
			result, rpcErr := handle(ctx, msg.Method, msg.Params)
			if len(msg.ID) == 0 || string(msg.ID) == "null" {
				return
			}
			response := map[string]any{"jsonrpc": "2.0", "id": rawID(msg.ID)}
			if rpcErr != nil {
				response["error"] = rpcErr
			} else {
				response["result"] = result
			}
			_ = c.write(response)
		}(msg, slot)
	}
	cancel()
	workers.Wait()
	c.flushWrites(100 * time.Millisecond)
	c.failPending()
	select {
	case err := <-c.writeErr:
		return fmt.Errorf("acp: write message: %w", err)
	default:
	}
	if err := parent.Err(); err != nil {
		return err
	}
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return fmt.Errorf("acp: read message: %w", readErr)
	}
	return nil
}

// readFrame consumes an oversized line through its delimiter, so the next
// JSON-RPC message (including cancellation) starts at a clean boundary.
func readFrame(r *bufio.Reader, limit int) ([]byte, error) {
	var data []byte
	oversized := false
	for {
		part, err := r.ReadSlice('\n')
		part = bytes.TrimSuffix(part, []byte{'\n'})
		if !oversized {
			if len(data)+len(part) > limit {
				oversized = true
				data = nil
			} else if err == nil && len(data) == 0 {
				return part, nil
			} else {
				data = append(data, part...)
			}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		if oversized {
			return nil, errFrameTooLarge
		}
		if len(data) == 0 && errors.Is(err, io.EOF) {
			return nil, io.EOF
		}
		return data, nil
	}
}

func (c *connection) flushWrites(timeout time.Duration) {
	done := make(chan error, 1)
	select {
	case c.writeQ <- outboundMessage{done: done}:
	default:
		return
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	}
}

func (c *connection) notify(method string, params any) error {
	return c.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (c *connection) request(ctx context.Context, method string, params any, result any) error {
	id := c.nextID.Add(1)
	key := fmt.Sprintf("%d", id)
	reply := make(chan message, 1)
	c.mu.Lock()
	c.pending[key] = reply
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, key)
		c.mu.Unlock()
	}()

	if err := c.writeContext(ctx, map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return err
	}
	select {
	case msg, ok := <-reply:
		if !ok {
			return io.EOF
		}
		if msg.Error != nil {
			return fmt.Errorf("acp client: %s", msg.Error.Message)
		}
		if result == nil || len(msg.Result) == 0 || string(msg.Result) == "null" {
			return nil
		}
		if err := json.Unmarshal(msg.Result, result); err != nil {
			return fmt.Errorf("acp: decode %s response: %w", method, err)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *connection) write(value any) error {
	return c.writeContext(context.Background(), value)
}

func (c *connection) writeContext(ctx context.Context, value any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if c.writeQ != nil {
		done := make(chan error, 1)
		message := outboundMessage{ctx: ctx, body: b, done: done}
		// Prefer an immediately available queue slot even if shutdown raced
		// with a handler finishing its response.
		select {
		case c.writeQ <- message:
		default:
			select {
			case c.writeQ <- message:
			case <-ctx.Done():
				return ctx.Err()
			case <-c.ctxDone:
				return context.Canceled
			case <-c.stopQ:
				return io.ErrClosedPipe
			}
		}
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			return ctx.Err()
		case <-c.ctxDone:
			return context.Canceled
		case <-c.stopQ:
			return io.ErrClosedPipe
		}
	}
	return c.writeDirect(b)
}

// tryWrite never waits for the peer. It is used by the reader loop so a peer
// that stops consuming output cannot prevent cancellation messages arriving.
func (c *connection) tryWrite(value any) bool {
	b, err := json.Marshal(value)
	if err != nil {
		return false
	}
	b = append(b, '\n')
	if c.writeQ == nil {
		return c.writeDirect(b) == nil
	}
	select {
	case c.writeQ <- outboundMessage{body: b}:
		return true
	default:
		if c.cancel != nil {
			c.cancel()
		}
		return false
	}
}

func (c *connection) writeLoop() {
	for {
		select {
		case message := <-c.writeQ:
			if message.ctx != nil && message.ctx.Err() != nil {
				message.done <- message.ctx.Err()
				continue
			}
			var err error
			if len(message.body) != 0 {
				err = c.writeDirect(message.body)
			}
			if message.done != nil {
				message.done <- err
			}
			if err != nil {
				select {
				case c.writeErr <- err:
				default:
				}
				if c.cancel != nil {
					c.cancel()
				}
				return
			}
		case <-c.stopQ:
			return
		}
	}
}

func (c *connection) writeDirect(b []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err := c.out.Write(b)
	return err
}

func (c *connection) resolve(msg message) {
	key := string(msg.ID)
	c.mu.Lock()
	reply := c.pending[key]
	c.mu.Unlock()
	if reply != nil {
		select {
		case reply <- msg:
		default:
		}
	}
}

func (c *connection) failPending() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, reply := range c.pending {
		close(reply)
		delete(c.pending, key)
	}
}

func rawID(id json.RawMessage) any {
	if len(id) == 0 {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(id))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return nil
	}
	return value
}
