// Package debug provides a global debug logger for the bot subsystem.
package debug

import (
	"fmt"
	"os"
	"sync"
	"time"
)

const (
	defaultLogDir  = "/tmp/nekocode"
	defaultLogFile = "nekocode-debug.log"
	maxSize        = 10 << 20
)

type Logger struct {
	mu   sync.Mutex
	file *os.File
	path string
	now  func() time.Time
}

func NewLogger(path string) *Logger {
	return &Logger{path: path, now: time.Now}
}

var defaultLogger = NewLogger(defaultPath())

// Log writes a timestamped, caller-annotated debug message.
func Log(format string, args ...any) {
	defaultLogger.Log(2, "[DBG]", "", format, args...)
}

// Sub returns a logger that prefixes messages with a subagent tag.
func Sub(name string) func(format string, args ...any) {
	return func(format string, args ...any) {
		defaultLogger.Log(3, "[SUB]", "["+name+"] ", format, args...)
	}
}

func (l *Logger) Log(skip int, level, prefix, format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	lf := l.logFile()
	if lf == nil {
		return
	}
	now := l.now
	if now == nil {
		now = time.Now
	}
	ts := now().Format("15:04:05.000")
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(lf, "%s %s %s%s %s\n", ts, level, prefix, callerFileLine(skip), msg)
}

func defaultPath() string {
	return defaultLogDir + "/" + defaultLogFile
}
