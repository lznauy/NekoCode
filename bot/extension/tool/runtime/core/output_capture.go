package core

import (
	"errors"
	"fmt"
	"io"
	"nekocode/protocol"
	"os"
	"strings"
	"sync"
)

// MaxCapturedOutputBytes bounds a subprocess result on disk as well as in
// memory. After the cap the producer is stopped; saved output is a prefix.
const MaxCapturedOutputBytes = 8 * 1024 * 1024

var ErrOutputLimit = errors.New("tool output capture limit reached")

// OutputCapture buffers small results and spills larger ones into its actor's
// store. Its preview is bounded independently of both chunk and result size.
type OutputCapture struct {
	mu       sync.Mutex
	store    *OutputStore
	limit    int
	stop     func()
	small    []byte
	head     []byte
	tail     []byte
	file     *os.File
	size     int
	limited  bool
	err      error
	finished bool
	result   ToolOutput
}

func NewOutputCapture(store *OutputStore, limit int, stop func()) *OutputCapture {
	return &OutputCapture{store: store, limit: max(0, limit), stop: stop}
}

func (c *OutputCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.finished {
		return 0, os.ErrClosed
	}
	if c.err != nil {
		return 0, c.err
	}
	if c.limited {
		return 0, ErrOutputLimit
	}
	n := min(len(p), c.limit-c.size)
	data := p[:n]
	if c.file == nil && c.size+n > MaxOutputBytes {
		c.file, c.err = c.store.createOutput()
		if c.err == nil {
			_, c.err = c.file.Write(c.small)
		}
		c.small = nil
	}
	if c.err != nil {
		return 0, c.err
	}
	if c.file != nil {
		written, err := c.file.Write(data)
		if err == nil && written != len(data) {
			err = io.ErrShortWrite
		}
		if err != nil {
			c.err = err
			return written, err
		}
	} else {
		c.small = append(c.small, data...)
	}
	const half = MaxOutputBytes / 2
	if len(c.head) < half {
		take := min(half-len(c.head), len(data))
		c.head = append(c.head, data[:take]...)
	}
	if len(data) >= half {
		c.tail = append(c.tail[:0], data[len(data)-half:]...)
	} else {
		overflow := max(0, len(c.tail)+len(data)-half)
		c.tail = append(c.tail[:copy(c.tail, c.tail[overflow:])], data...)
	}
	c.size += n
	if n < len(p) {
		c.limited = true
		if c.stop != nil {
			c.stop()
		}
		return n, ErrOutputLimit
	}
	return n, nil
}

// Finish closes and syncs the file before its reference can enter a session.
func (c *OutputCapture) Finish() (ToolOutput, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.finished {
		return c.result, c.err
	}
	c.finished = true
	if c.file == nil {
		c.result.Text = string(c.small)
	} else {
		if err := c.file.Sync(); c.err == nil {
			c.err = err
		}
		if err := c.file.Close(); c.err == nil {
			c.err = err
		}
		header := OutputReferenceHeader(c.file.Name(), !c.limited)
		preview := strings.ToValidUTF8(string(c.head), "") + outputTruncated + strings.ToValidUTF8(string(c.tail), "")
		c.result = ToolOutput{Text: LimitOutput(preview, MaxOutputBytes-len(header)-256), Artifact: protocol.ToolOutputArtifact{Path: c.file.Name(), Incomplete: c.limited}}
	}
	if c.limited {
		c.result.Text += fmt.Sprintf("\n[Search stopped after %d output bytes. Saved output is incomplete; narrow path, glob or pattern.]", c.size)
	}
	return c.result, c.err
}

func (c *OutputCapture) Limited() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.limited
}
