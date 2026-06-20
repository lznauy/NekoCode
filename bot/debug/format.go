package debug

import (
	"fmt"
	"runtime"
	"strings"
)

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
