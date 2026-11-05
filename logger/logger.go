// Package logger provides the project's rotating diagnostic log.
package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"nekocode/util/fs"
)

const (
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
	defaultLogger.Log(3, "DBG", "", format, args...)
}

// Sub returns a logger that prefixes messages with a subagent tag.
func Sub(name string) func(format string, args ...any) {
	return func(format string, args ...any) {
		defaultLogger.Log(3, "SUB", "["+name+"] ", format, args...)
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
	if _, err := fmt.Fprintf(lf, "%s %-3s %-36s | %s%s\n", ts, level, callerFileLine(skip), prefix, msg); err != nil {
		_ = lf.Close()
		l.file = nil
	}
}

func (l *Logger) logFile() *os.File {
	if l.file != nil {
		info, err := l.file.Stat()
		if err == nil && info.Size() <= maxSize {
			return l.file
		}
		_ = l.file.Close()
		l.file = nil
	}
	if l.path == "" {
		l.path = defaultPath()
	}
	dir := filepath.Dir(l.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil
	}
	_ = os.Chmod(dir, 0o700)
	rotateIfNeeded(l.path, maxSize)
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil
	}
	_ = f.Chmod(0o600)
	l.file = f
	return l.file
}

func defaultPath() string {
	return filepath.Join(fs.NekocodeLogDir(), defaultLogFile)
}

func callerFileLine(skip int) string {
	_, file, line, ok := runtime.Caller(skip)
	if !ok {
		return "?:?"
	}
	return fmt.Sprintf("%s:%d", trimPath(file), line)
}

func trimPath(path string) string {
	path = filepath.ToSlash(path)
	if cwd, err := os.Getwd(); err == nil {
		cwd = filepath.ToSlash(cwd)
		if rel, err := filepath.Rel(cwd, path); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	if idx := strings.LastIndex(path, "/NekoCode/"); idx >= 0 {
		return path[idx+len("/NekoCode/"):]
	}
	parts := strings.Split(path, "/")
	if len(parts) >= 2 {
		return strings.Join(parts[len(parts)-2:], "/")
	}
	return path
}

func rotateIfNeeded(path string, maxBytes int64) {
	fi, err := os.Stat(path)
	if err == nil && fi.Size() > maxBytes {
		rotated := path + ".1"
		if os.Rename(path, rotated) == nil {
			_ = os.Chmod(rotated, 0o600)
		}
	}
}
