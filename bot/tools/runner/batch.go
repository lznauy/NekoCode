package runner

import (
	"context"
	"sync"

	"nekocode/bot/tools/core"
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
	if len(calls) == 0 {
		return
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 16)
	for _, c := range calls {
		if ctx.Err() != nil {
			results[c.idx] = core.ToolCallResult{ID: c.call.ID, Name: c.call.Name, Error: ctx.Err().Error()}
			continue
		}
		wg.Add(1)
		go func(ic indexedCall) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[ic.idx] = e.executeOne(ctx, ic.call)
		}(c)
	}
	wg.Wait()
}

func (e *Executor) executeSequential(ctx context.Context, calls []indexedCall, results []core.ToolCallResult) {
	for _, c := range calls {
		e.emitPreview(c.call)
		results[c.idx] = e.executeOne(ctx, c.call)
	}
}
