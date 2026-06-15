package common

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"
	"time"
)

const panicLogDir = "/tmp/nekocode"

// WritePanicLog writes a panic recovery log to /tmp/nekocode/.
// Call from defer/recover blocks in both cmd and TUI.
func WritePanicLog(recoverVal any) {
	stack := string(debug.Stack())
	if err := os.MkdirAll(panicLogDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create panic log dir: %v\n\nPANIC: %v\nStack:\n%s\n", err, recoverVal, stack)
		return
	}
	logPath := fmt.Sprintf("%s/nekocode-panic-%d.log", panicLogDir, time.Now().Unix())
	if err := os.WriteFile(logPath, fmt.Appendf(nil, "PANIC: %v\n\nStack:\n%s", recoverVal, stack), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write panic log: %v\n", err)
	}
	fmt.Fprintf(os.Stderr, "\nPANIC: %v\nStack saved to %s\n", recoverVal, logPath)
}

// ShortContext returns a context with a 10-second timeout.
// Suitable for short operations like HTTP fetches, plugin commands, etc.
func ShortContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}
