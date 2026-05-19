package compact

import (
	"fmt"
	"os"
	"sync"
)

var (
	logMu   sync.Mutex
	logFile *os.File
)

func Log(format string, args ...any) {
	compactLog(format, args...)
}

func compactLog(format string, args ...any) {
	logMu.Lock()
	defer logMu.Unlock()
	if logFile == nil {
		f, err := os.OpenFile("/tmp/nekocode-debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return
		}
		logFile = f
	}
	fmt.Fprintf(logFile, format+"\n", args...)
}
