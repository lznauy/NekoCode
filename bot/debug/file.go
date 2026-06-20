package debug

import (
	"os"
	"path/filepath"
)

func (l *Logger) logFile() *os.File {
	if l.file != nil {
		return l.file
	}
	if l.path == "" {
		l.path = defaultPath()
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0755); err != nil {
		return nil
	}
	rotateIfNeeded(l.path, maxSize)
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil
	}
	l.file = f
	return l.file
}

func rotateIfNeeded(path string, maxBytes int64) {
	fi, err := os.Stat(path)
	if err == nil && fi.Size() > maxBytes {
		os.Rename(path, path+".1")
	}
}
