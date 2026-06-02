// Package debug provides a global debug logger for the bot subsystem.
// Log output goes to /tmp/nekocode/nekocode-debug.log (rotated at 10MB).
package debug

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

const maxSize = 10 << 20 // 10MB

var (
	mu sync.Mutex
	f  *os.File
)

func logFile() *os.File {
	if f != nil {
		return f
	}
	if err := os.MkdirAll("/tmp/nekocode", 0755); err != nil {
		return nil
	}
	fi, err := os.Stat("/tmp/nekocode/nekocode-debug.log")
	if err == nil && fi.Size() > maxSize {
		os.Rename("/tmp/nekocode/nekocode-debug.log", "/tmp/nekocode/nekocode-debug.log.1")
	}
	f, err = os.OpenFile("/tmp/nekocode/nekocode-debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil
	}
	return f
}

func callerFileLine(skip int) string {
	_, file, line, ok := runtime.Caller(skip)
	if !ok {
		return "?:?"
	}
	if idx := strings.LastIndexByte(file, '/'); idx >= 0 {
		file = file[idx+1:]
	}
	return fmt.Sprintf("%s:%d", file, line)
}

// Log writes a timestamped, caller-annotated debug message.
func Log(format string, args ...any) {
	mu.Lock()
	defer mu.Unlock()
	lf := logFile()
	if lf == nil {
		return
	}
	ts := time.Now().Format("15:04:05.000")
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(lf, "%s [DBG] %s %s\n", ts, callerFileLine(2), msg)
}

// Sub returns a logger that prefixes messages with a subagent tag.
func Sub(name string) func(format string, args ...any) {
	return func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		lf := logFile()
		if lf == nil {
			return
		}
		ts := time.Now().Format("15:04:05.000")
		msg := fmt.Sprintf(format, args...)
		fmt.Fprintf(lf, "%s [SUB] [%s] %s %s\n", ts, name, callerFileLine(3), msg)
	}
}
