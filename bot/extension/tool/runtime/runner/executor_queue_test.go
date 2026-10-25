package runner

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/toolutil"
)

type queueGateTool struct {
	toolutil.SafeReadOnlyTool
	name string
	run  func(context.Context) error
}

func (t *queueGateTool) Name() string                 { return t.name }
func (t *queueGateTool) Description() string          { return "test queue" }
func (t *queueGateTool) Parameters() []core.Parameter { return nil }
func (t *queueGateTool) Execute(ctx context.Context, _ map[string]any) (core.ToolOutput, error) {
	return core.ToolOutput{Text: "done"}, t.run(ctx)
}

func TestTaskQueueBoundsHundredTasksAndDoesNotBlockOrdinaryTools(t *testing.T) {
	for _, configured := range []int{0, 1, 3, 12} {
		limit := configured
		if limit == 0 {
			limit = 8
		}
		for _, cancelQueued := range []bool{false, true} {
			t.Run(fmt.Sprintf("limit%d/cancel%v", configured, cancelQueued), func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				gate := make(chan struct{})
				started := make(chan struct{}, 100)
				ordinary := make(chan struct{}, 1)
				var active, peak, total atomic.Int32
				delegate := &queueGateTool{name: "task", run: func(ctx context.Context) error {
					n := active.Add(1)
					defer active.Add(-1)
					for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
					}
					total.Add(1)
					started <- struct{}{}
					select {
					case <-gate:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				}}
				regular := &queueGateTool{name: "ordinary", run: func(context.Context) error { ordinary <- struct{}{}; return nil }}
				executor := NewExecutor(newTestRegistry(fakeRegistry{"task": delegate, "ordinary": regular}))
				executor.SetMaxConcurrentTasks(configured)
				calls := make([]core.ToolCallItem, 0, 101)
				for i := 0; i < 100; i++ {
					calls = append(calls, core.ToolCallItem{ID: fmt.Sprint(i), Name: "task"})
				}
				calls = append(calls, core.ToolCallItem{ID: "ordinary", Name: "ordinary"})
				done := make(chan []core.ToolCallResult, 1)
				go func() { done <- executor.ExecuteBatch(ctx, calls) }()
				for i := 0; i < limit; i++ {
					select {
					case <-started:
					case <-time.After(3 * time.Second):
						t.Fatal("task pool stalled")
					}
				}
				select {
				case <-ordinary:
				case <-time.After(3 * time.Second):
					t.Fatal("queued tasks starved ordinary tools")
				}
				if total.Load() != int32(limit) {
					t.Fatalf("started %d tasks before capacity freed", total.Load())
				}
				if cancelQueued {
					cancel()
				} else {
					close(gate)
				}
				var results []core.ToolCallResult
				select {
				case results = <-done:
				case <-time.After(3 * time.Second):
					t.Fatal("queue failed to drain")
				}
				if peak.Load() != int32(limit) || active.Load() != 0 {
					t.Fatalf("peak=%d active=%d", peak.Load(), active.Load())
				}
				want := int32(100)
				if cancelQueued {
					want = int32(limit)
				}
				if total.Load() != want {
					t.Fatalf("executed %d, want %d", total.Load(), want)
				}
				for i, result := range results {
					if result.ID != calls[i].ID {
						t.Fatal("queue reordered result association")
					}
					if !cancelQueued && result.Error != "" {
						t.Fatalf("queued task failed: %+v", result)
					}
					if cancelQueued && i < 100 && result.Error == "" {
						t.Fatalf("cancelled task lost cancellation: %+v", result)
					}
				}
			})
		}
	}
}
