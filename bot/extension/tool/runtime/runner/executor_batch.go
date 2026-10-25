package runner

import (
	"context"
	"sync"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/protocol"
)

type indexedCall struct {
	idx  int
	call core.ToolCallItem
}

func (e *Executor) ExecuteBatch(ctx context.Context, calls []core.ToolCallItem) []core.ToolCallResult {
	parallel, sequential := e.partitionCalls(calls)
	results := make([]core.ToolCallResult, len(calls))

	e.executeParallel(ctx, parallel, results)
	e.executeSequential(ctx, sequential, results)

	return results
}

func (e *Executor) partitionCalls(calls []core.ToolCallItem) ([]indexedCall, []indexedCall) {
	var parallel, sequential []indexedCall
	for i, c := range calls {
		if t, err := e.registry.Get(c.Name); err == nil && t.ExecutionMode(c.Args) == core.ModeParallel {
			parallel = append(parallel, indexedCall{idx: i, call: c})
		} else {
			sequential = append(sequential, indexedCall{idx: i, call: c})
		}
	}
	return parallel, sequential
}

func (e *Executor) executeParallel(ctx context.Context, calls []indexedCall, results []core.ToolCallResult) {
	var tasks, ordinary []indexedCall
	for _, call := range calls {
		if call.call.Name == "task" {
			tasks = append(tasks, call)
		} else {
			ordinary = append(ordinary, call)
		}
	}
	var wg sync.WaitGroup
	e.fnMu.RLock()
	limit := e.maxConcurrentTasks
	e.fnMu.RUnlock()
	if limit <= 0 {
		limit = taskbridge.DefaultMaxConcurrentTasks
	}
	wg.Add(1)
	go func() { defer wg.Done(); e.executeWorkers(ctx, tasks, results, limit) }()
	e.executeWorkers(ctx, ordinary, results, 16)
	wg.Wait()
}

// A bounded worker pool keeps waiting tasks out of the ordinary tool pool and
// consumes the input queue in order without one goroutine per queued call.
func (e *Executor) executeWorkers(ctx context.Context, calls []indexedCall, results []core.ToolCallResult, limit int) {
	if len(calls) == 0 {
		return
	}
	var wg sync.WaitGroup
	jobs := make(chan indexedCall)
	for i := 0; i < min(limit, len(calls)); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ic := range jobs {
				if err := ctx.Err(); err != nil {
					results[ic.idx] = core.ToolCallResult{ID: ic.call.ID, Name: ic.call.Name, Error: err.Error()}
					finishQueuedTask(ic.call, "interrupted")
					continue
				}
				results[ic.idx] = e.executeOne(ctx, ic.call)
				status := "completed"
				if results[ic.idx].Error != "" {
					status = "failed"
				}
				if ctx.Err() != nil {
					status = "interrupted"
				}
				finishQueuedTask(ic.call, status)
			}
		}()
	}
	for _, call := range calls {
		jobs <- call
	}
	close(jobs)
	wg.Wait()
}

func finishQueuedTask(call core.ToolCallItem, status string) {
	if call.Name != "task" {
		return
	}
	if callback, ok := call.Args["_sub_callback"].(taskbridge.TaskCallback); ok && callback.Finish != nil {
		callback.Finish(protocol.StepEvent{Action: protocol.StepActionSubAgentEnd, SubAgentStatus: status})
	}
}

func (e *Executor) executeSequential(ctx context.Context, calls []indexedCall, results []core.ToolCallResult) {
	for _, c := range calls {
		e.emitPreview(ctx, c.call)
		results[c.idx] = e.executeOne(ctx, c.call)
	}
}
