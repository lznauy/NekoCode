package feishu

import (
	"context"
	"sync"
	"testing"
)

// TestDeliverFileConcurrentWithStop guards the api-snapshot race: DeliverFile
// fires from a tool mid-run while a management surface Stops the connector.
// Under -race, unsynchronized access to c.api would fail here. The call may
// fail for config reasons — the test only cares about the memory access
// pattern.
func TestDeliverFileConcurrentWithStop(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	c := New(nil)
	// Simulate a running connector without the websocket loop.
	c.api = newFeishuClient("app", "secret")

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = c.DeliverFile(context.Background(), "testdata/no-such-file.png")
		}()
		go func() {
			defer wg.Done()
			c.apiMu.Lock()
			c.api = nil
			c.apiMu.Unlock()
		}()
	}
	wg.Wait()
}
