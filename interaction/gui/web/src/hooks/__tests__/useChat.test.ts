import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useChat } from '../useChat'

const listeners: Record<string, Array<(...args: unknown[]) => void>> = {}

function emit(event: string, data: unknown): void {
  listeners[event]?.forEach((cb) => cb(data))
}

 beforeEach(() => {
  Object.keys(listeners).forEach((k) => delete listeners[k])
	delete (window as unknown as { go?: unknown }).go

  vi.stubGlobal('runtime', {
    EventsOnMultiple: vi.fn((event: string, cb: (...args: unknown[]) => void) => {
      if (!listeners[event]) listeners[event] = []
      listeners[event].push(cb)
      return () => {
        listeners[event] = listeners[event].filter((l) => l !== cb)
      }
    }),
  })
})

describe('useChat', () => {
  it.each(['edit', 'replace', 'stop'])('keeps submission ownership after automatic review and %s', async (action) => {
    let rejectSend!: (error: Error) => void
    ;(window as unknown as { go: unknown }).go = { main: { App: {
      SendMessage: () => new Promise<void>((_, reject) => { rejectSend = reject }),
      Abort: vi.fn().mockResolvedValue(undefined),
    } } }
    const { result } = renderHook(() => useChat())
    act(() => result.current.send('/compact retry'))
    act(() => emit('agent:input_accepted', { source: { kind: 'task_completion' } }))
    act(() => {
      if (action === 'replace') result.current.clearMessages()
      if (action === 'stop') result.current.stop()
      result.current.setText('new draft')
    })
    await act(async () => rejectSend(new Error('old Busy')))
    expect(result.current.text).toBe('new draft')
    if (action === 'edit') expect(result.current.error).toContain('old Busy')
    else if (action === 'replace') expect(result.current.error).toBeNull()
    else expect(result.current.msgs.some(message => message.text?.includes('old Busy'))).toBe(false)
  })

  it('restores image ownership when a command fails after an automatic review', async () => {
    let rejectSend!: (error: Error) => void
    const remove = vi.fn().mockResolvedValue(undefined)
    ;(window as unknown as { go: unknown }).go = { main: { App: {
      SendMessage: () => new Promise<void>((_, reject) => { rejectSend = reject }),
      SaveClipboardImage: vi.fn().mockResolvedValue('/tmp/review-draft.png'),
      DeleteClipboardImage: remove,
    } } }
    const { result } = renderHook(() => useChat())
    await act(async () => result.current.pasteImages([new File(['png'], 'shot.png', { type: 'image/png' })], 0, 0))
    act(() => result.current.send('/compact'))
    act(() => {
      emit('agent:input_accepted', { source: { kind: 'task_completion' } })
      emit('agent:done', { output: 'review', error: '' })
    })
    await act(async () => rejectSend(new Error('Busy')))
    expect(result.current.text).toBe('[Image #1]')
    expect(result.current.imageAttachments).toEqual([{ label: '[Image #1]', path: '/tmp/review-draft.png' }])
    expect(remove).not.toHaveBeenCalled()
  })

  it.each([false, true])('restores a rejected command across automatic review (done first: %s)', async (doneFirst) => {
    let rejectSend!: (error: Error) => void
    ;(window as unknown as { go: unknown }).go = { main: { App: { SendMessage: () => new Promise<void>((_, reject) => { rejectSend = reject }) } } }
    const { result } = renderHook(() => useChat())
    act(() => result.current.setText('/compact retry'))
    act(() => result.current.send())
    act(() => {
      emit('agent:input_accepted', { source: { kind: 'task_completion' } })
      if (doneFirst) emit('agent:done', { output: 'review', error: '' })
    })
    await act(async () => rejectSend(new Error('Busy')))
    expect(result.current.text).toBe('/compact retry')
    expect(result.current.error).toContain('Busy')
    if (!doneFirst) {
      expect(result.current.busy).toBe(true)
      act(() => emit('agent:done', { output: 'review', error: '' }))
    }
    expect(result.current.msgs.some(message => message.text === 'review')).toBe(true)
    expect(result.current.msgs.some(message => message.text?.includes('Error: Error: Busy'))).toBe(true)
  })

  it.each([false, true])('settles a local send queued behind a coordinator (review first: %s)', (reviewFirst) => {
    const { result } = renderHook(() => useChat())
    act(() => result.current.send('queued local demand'))
    if (reviewFirst) {
      act(() => {
        emit('agent:input_accepted', { source: { kind: 'task_completion' } })
        emit('agent:done', { output: 'background review', error: '' })
      })
    }
    act(() => {
      emit('agent:input_accepted', { source: { kind: 'gui' }, queued: true, content: 'queued local demand' })
      emit('agent:done', { output: 'local reply', error: '' })
    })
    expect(result.current.msgs.filter(message => message.role === 'user').map(message => message.text)).toEqual(['queued local demand'])
    expect(result.current.msgs.filter(message => message.streaming)).toHaveLength(0)
    expect(result.current.msgs.filter(message => message.role === 'assistant').map(message => message.text)).toEqual(reviewFirst ? ['background review', 'local reply'] : ['local reply'])
  })

  it('creates a queued demand turn only on admission, preserving the preceding reply', () => {
    const { result } = renderHook(() => useChat())
    act(() => result.current.send('first demand'))
    act(() => {
      emit('agent:delta', { id: 1, delta: 'first reply', done: false })
      emit('agent:done', { output: 'first reply', error: '' })
      emit('agent:status', { status: 'idle' })
      emit('agent:status', { status: 'running' })
      emit('agent:input_accepted', { source: { kind: 'tui' }, queued: true, content: 'queued demand' })
      emit('agent:delta', { id: 2, delta: 'queued reply', done: false })
      emit('agent:done', { output: 'queued reply', error: '' })
    })
    expect(result.current.msgs.filter(message => message.role === 'user').map(message => message.text)).toEqual(['first demand', 'queued demand'])
    expect(result.current.msgs.filter(message => message.role === 'assistant').map(message => message.text)).toEqual(['first reply', 'queued reply'])
  })

  it('renders an automatic task review as a new assistant response', async () => {
    const { result } = renderHook(() => useChat())
    act(() => {
      emit('agent:status', { status: 'running' })
      emit('agent:input_accepted', { source: { kind: 'task_completion' } })
      emit('agent:delta', { id: 1, delta: 'Verified background result', done: false })
      emit('agent:done', { output: 'Verified background result', error: '' })
      emit('agent:status', { status: 'idle' })
    })
    expect(result.current.msgs.filter(message => message.role === 'assistant').map(message => message.text)).toEqual(['Verified background result'])
    expect(result.current.msgs.filter(message => message.role === 'user')).toHaveLength(0)
  })

  it('does not cancel until the pending start has been acknowledged', async () => {
    let finishStart!: () => void
    const abort = vi.fn().mockResolvedValue(undefined)
    ;(window as unknown as { go: unknown }).go = { main: { App: { SendMessage: () => new Promise<void>(resolve => { finishStart = resolve }), Abort: abort } } }
    const { result } = renderHook(() => useChat())
    act(() => result.current.send('pending'))
    act(() => result.current.stop())
    expect(abort).not.toHaveBeenCalled()
    await act(async () => finishStart())
    expect(abort).toHaveBeenCalledTimes(1)
    await act(async () => emit('agent:status', { status: 'idle' }))
    expect(result.current.busy).toBe(false)
  })

  it('blocks new submissions until the pending Abort is acknowledged', async () => {
    let finishAbort!: () => void
    const send = vi.fn().mockResolvedValue(undefined)
    const abort = vi.fn(() => new Promise<void>(resolve => { finishAbort = resolve }))
    ;(window as unknown as { go: unknown }).go = { main: { App: { SendMessage: send, Abort: abort } } }
    const { result } = renderHook(() => useChat())
    act(() => result.current.send('old'))
    act(() => result.current.stop())
    await act(async () => { await Promise.resolve() })
    expect(abort).toHaveBeenCalledTimes(1)
    act(() => { emit('agent:done', { output: '', error: '' }); emit('agent:status', { status: 'idle' }) })
    expect(result.current.busy).toBe(true)
    act(() => result.current.send('too early'))
    expect(send).toHaveBeenCalledTimes(1)
    await act(async () => finishAbort())
    expect(result.current.busy).toBe(false)
    act(() => result.current.send('new'))
    expect(send).toHaveBeenCalledTimes(2)
  })

  it('settles cancellation after replacing messages while Abort is pending', async () => {
    let finishAbort!: () => void
    const send = vi.fn().mockResolvedValue(undefined)
    ;(window as unknown as { go: unknown }).go = { main: { App: {
      SendMessage: send, Abort: () => new Promise<void>(resolve => { finishAbort = resolve }),
    } } }
    const { result } = renderHook(() => useChat())
    act(() => result.current.send('old'))
    act(() => result.current.stop())
    await act(async () => { await Promise.resolve() })
    act(() => emit('agent:status', { status: 'idle' }))
    act(() => result.current.setMessages([]))
    expect(result.current.busy).toBe(true)
    act(() => result.current.send('too early'))
    expect(send).toHaveBeenCalledTimes(1)
    await act(async () => finishAbort())
    expect(result.current.busy).toBe(false)
    expect(result.current.stopping).toBe(false)
  })

  it('unlocks a cancelled submission when startup fails without terminal events', async () => {
    let rejectStart!: (error: Error) => void
    const abort = vi.fn().mockResolvedValue(undefined)
    ;(window as unknown as { go: unknown }).go = { main: { App: { SendMessage: () => new Promise<void>((_, reject) => { rejectStart = reject }), Abort: abort } } }
    const { result } = renderHook(() => useChat())
    act(() => result.current.send('pending'))
    act(() => result.current.stop())
    await act(async () => rejectStart(new Error('startup failed')))
    expect(result.current.busy).toBe(false)
    expect(result.current.stopping).toBe(false)
    expect(result.current.error).toContain('startup failed')
    expect(abort).not.toHaveBeenCalled()
  })

  it('waits for cancellation completion before accepting another run', async () => {
    const send = vi.fn().mockResolvedValue(undefined)
    ;(window as unknown as { go: unknown }).go = { main: { App: { SendMessage: send, Abort: vi.fn().mockResolvedValue(undefined) } } }
    const { result } = renderHook(() => useChat())
    act(() => result.current.send('old'))
    act(() => result.current.stop())
    expect(result.current.busy).toBe(true)
    act(() => result.current.send('too early'))
    expect(send).toHaveBeenCalledTimes(1)
    await act(async () => { emit('agent:done', { output: '', error: 'cancelled' }); emit('agent:status', { status: 'idle' }) })
    expect(result.current.busy).toBe(false)
    act(() => result.current.send('new'))
    act(() => emit('agent:tool_start', { id: 'new-tool', toolName: 'shell', args: '' }))
    expect(result.current.msgs[1].steps).toHaveLength(1)
    expect(result.current.busy).toBe(true)
  })

  it('ignores rejection from a replaced session after a new run starts', async () => {
    let rejectOld!: (reason: Error) => void
    const send = vi.fn().mockImplementationOnce(() => new Promise<void>((_, reject) => { rejectOld = reject })).mockResolvedValue(undefined)
    ;(window as unknown as { go: unknown }).go = { main: { App: { SendMessage: send, Abort: vi.fn() } } }
    const { result } = renderHook(() => useChat())
    act(() => result.current.send('old'))
    act(() => result.current.setMessages([]))
    act(() => emit('agent:status', { status: 'idle' }))
    act(() => result.current.send('new'))
    await act(async () => { rejectOld(new Error('old rejected')); await Promise.resolve() })
    expect(result.current.busy).toBe(true)
    expect(result.current.error).toBeNull()
    act(() => emit('agent:tool_start', { id: 'new-tool', toolName: 'shell', args: '' }))
    expect(result.current.msgs[1].steps).toHaveLength(1)
    expect(result.current.msgs).toHaveLength(2)
  })

  it.each(['running shell', 'Running process', 'running bash'])('normalizes tool phase %s to a displayable state', (phase) => {
    const { result } = renderHook(() => useChat())
    act(() => result.current.send('run tool'))
    act(() => emit('agent:phase', { phase }))
    expect(result.current.msgs[1].phase).toBe('running')
    act(() => emit('agent:phase', { phase: 'unexpected phase' }))
    expect(result.current.msgs[1].phase).toBe('running')
  })

  it.each(['process', 'bash'])('retains %s output and steps when the run finishes', (toolName) => {
    const { result } = renderHook(() => useChat())
    act(() => result.current.send('run tool'))
    act(() => {
      emit('agent:tool_start', { id: 'execution', toolName, args: '' })
      emit('agent:tool_done', { id: 'execution', toolName, args: '', output: 'persistent execution output', isError: false })
    })
    expect(result.current.msgs[1].steps?.[0].output).toBe('persistent execution output')
    act(() => emit('agent:done', { output: 'finished', error: '' }))
    expect(result.current.msgs[1].steps).toHaveLength(1)
    expect(result.current.msgs[1].steps?.[0]).toMatchObject({ toolName, output: 'persistent execution output', status: 'done' })
  })

  it('clears pending stream timers and permits a fresh run after clearing messages', () => {
    vi.useFakeTimers()
    try {
      const { result } = renderHook(() => useChat())
      act(() => result.current.send('old run'))
      act(() => {
        emit('agent:delta', { id: 1, delta: 'old buffered text', done: false })
        emit('agent:reasoning', { delta: 'old reasoning', done: false })
      })
      expect(vi.getTimerCount()).toBeGreaterThan(0)
      act(() => result.current.clearMessages())
      expect(vi.getTimerCount()).toBe(0)
      expect(result.current.msgs).toEqual([])
      act(() => {
        emit('agent:delta', { id: 1, delta: 'late old text', done: false })
        emit('agent:tool_start', { id: 'late', toolName: 'shell', args: '' })
        vi.runAllTimers()
        emit('agent:status', { status: 'idle' })
      })
      expect(result.current.msgs).toEqual([])
      expect(vi.getTimerCount()).toBe(0)
      // No old done event was sent: this also checks sendingRef was reset.
      act(() => result.current.send('new run'))
      act(() => {
        emit('agent:delta', { id: 2, delta: 'fresh text', done: false })
        vi.runAllTimers()
      })
      expect(result.current.msgs).toHaveLength(2)
      expect(result.current.msgs[0].text).toBe('new run')
      expect(result.current.msgs[1].streamText).toBe('fresh text')
      expect(result.current.msgs[1].reasoning).toBe('')
    } finally {
      vi.useRealTimers()
    }
  })

  it('clears the draft and error together with messages', () => {
    const { result } = renderHook(() => useChat())
    act(() => result.current.send('old run'))
    act(() => emit('agent:done', { output: '', error: 'old error' }))
    act(() => result.current.setText('old draft'))
    expect(result.current.error).toBe('old error')
    expect(result.current.text).toBe('old draft')
    act(() => result.current.clearMessages())
    expect(result.current.msgs).toEqual([])
    expect(result.current.text).toBe('')
    expect(result.current.error).toBeNull()
  })

	it('matches concurrent tool previews by call ID and ignores stale IDs', () => {
	  const { result } = renderHook(() => useChat())
	  act(() => result.current.send('inspect'))
	  act(() => {
	    emit('agent:tool_start', { id: 'a', toolName: 'edit', args: '', preview: 'first' })
	    emit('agent:tool_start', { id: 'b', toolName: 'edit', args: '', preview: 'second' })
	    emit('agent:tool_preview', { id: 'a', toolName: 'edit', preview: 'updated first' })
	    emit('agent:tool_preview', { id: 'stale', toolName: 'edit', preview: 'stale preview' })
	  })
	  expect(result.current.msgs[1].steps?.map((step) => step.preview)).toEqual(['updated first', 'second'])
	  act(() => {
	    emit('agent:tool_done', { id: 'a', toolName: 'edit', args: '', output: 'finished', isError: false })
	    emit('agent:tool_preview', { id: 'a', toolName: 'edit', preview: 'late preview' })
	  })
	  expect(result.current.msgs[1].steps?.map((step) => step.preview)).toEqual([undefined, 'second'])
	})

	it('retains latest-running preview matching for backends without call IDs', () => {
	  const { result } = renderHook(() => useChat())
	  act(() => result.current.send('inspect'))
	  act(() => {
	    emit('agent:tool_start', { id: 'a', toolName: 'edit', args: '', preview: 'first' })
	    emit('agent:tool_start', { id: 'b', toolName: 'edit', args: '', preview: 'second' })
	    emit('agent:tool_preview', { toolName: 'edit', preview: 'legacy preview' })
	  })
	  expect(result.current.msgs[1].steps?.map((step) => step.preview)).toEqual(['first', 'legacy preview'])
	})

	it('preserves unrelated messages when toggling a tool step', () => {
	  const { result } = renderHook(() => useChat())
	  act(() => result.current.setMessages([
	    { id: 'first', role: 'assistant', text: 'older', streaming: false, steps: [{ id: 'a', toolName: 'edit', args: '', status: 'done', isError: false, collapsed: true }] },
	    { id: 'second', role: 'assistant', text: 'newer', streaming: false, steps: [{ id: 'b', toolName: 'edit', args: '', status: 'done', isError: false, collapsed: true }] },
	  ]))
	  const before = result.current.msgs
	  act(() => result.current.toggleStep('b'))
	  expect(result.current.msgs[0]).toBe(before[0])
	  expect(result.current.msgs[1].steps?.[0].collapsed).toBe(false)
	  const unchanged = result.current.msgs
	  act(() => result.current.toggleStep('missing'))
	  expect(result.current.msgs).toBe(unchanged)
	})
	it('persists pasted images, inserts a placeholder, and sends the attachment', async () => {
	  const save = vi.fn().mockResolvedValue('/tmp/paste.png')
	  const remove = vi.fn().mockResolvedValue(undefined)
	  const send = vi.fn().mockResolvedValue(undefined)
	  ;(window as unknown as { go: unknown }).go = { main: { App: {
		SaveClipboardImage: save,
		DeleteClipboardImage: remove,
		SendMessage: send,
	  } } }
	  const { result } = renderHook(() => useChat())
	  const image = new File(['png'], 'shot.png', { type: 'image/png' })

	  await act(async () => {
		await result.current.pasteImages([image], 0, 0)
	  })
	  expect(result.current.text).toBe('[Image #1]')
	  expect(result.current.imageAttachments).toEqual([{ label: '[Image #1]', path: '/tmp/paste.png' }])

	  act(() => result.current.send())
	  expect(send).toHaveBeenCalledWith('[Image #1]', [{ label: '[Image #1]', path: '/tmp/paste.png' }])
	  expect(remove).not.toHaveBeenCalled()
	})

	it('restores an image draft when a control command finishes without accepting it', async () => {
	  const remove = vi.fn().mockResolvedValue(undefined)
	  ;(window as unknown as { go: unknown }).go = { main: { App: {
		SaveClipboardImage: vi.fn().mockResolvedValue('/tmp/draft.png'),
		DeleteClipboardImage: remove,
		SendMessage: vi.fn().mockResolvedValue(undefined),
	  } } }
	  const { result } = renderHook(() => useChat())
	  await act(async () => {
		await result.current.pasteImages([new File(['png'], 'shot.png', { type: 'image/png' })], 0, 0)
	  })
	  act(() => result.current.send('/compact'))
	  act(() => emit('agent:done', { output: '', error: '' }))
	  expect(result.current.text).toBe('[Image #1]')
	  expect(result.current.imageAttachments).toEqual([{ label: '[Image #1]', path: '/tmp/draft.png' }])
	  expect(remove).not.toHaveBeenCalled()
	})

	it('does not restore images after the runtime accepts them', async () => {
	  ;(window as unknown as { go: unknown }).go = { main: { App: {
		SaveClipboardImage: vi.fn().mockResolvedValue('/tmp/accepted.png'),
		DeleteClipboardImage: vi.fn().mockResolvedValue(undefined),
		SendMessage: vi.fn().mockResolvedValue(undefined),
	  } } }
	  const { result } = renderHook(() => useChat())
	  await act(async () => {
		await result.current.pasteImages([new File(['png'], 'shot.png', { type: 'image/png' })], 0, 0)
	  })
	  act(() => result.current.send())
	  act(() => emit('agent:input_accepted', {
		source: { kind: 'gui' },
		images: [{ label: '[Image #1]', path: '/tmp/accepted.png' }],
	  }))
	  act(() => emit('agent:done', { output: '', error: '' }))
	  expect(result.current.text).toBe('')
	  expect(result.current.imageAttachments).toEqual([])
	})

	it('restores an unaccepted image draft when the run is stopped', async () => {
	  ;(window as unknown as { go: unknown }).go = { main: { App: {
		SaveClipboardImage: vi.fn().mockResolvedValue('/tmp/stopped.png'),
		DeleteClipboardImage: vi.fn().mockResolvedValue(undefined),
		SendMessage: vi.fn().mockResolvedValue(undefined),
		Abort: vi.fn(),
	  } } }
	  const { result } = renderHook(() => useChat())
	  await act(async () => {
		await result.current.pasteImages([new File(['png'], 'shot.png', { type: 'image/png' })], 0, 0)
	  })
	  act(() => result.current.send('/compact'))
	  act(() => result.current.stop())
	  expect(result.current.text).toBe('[Image #1]')
	  expect(result.current.imageAttachments).toEqual([{ label: '[Image #1]', path: '/tmp/stopped.png' }])
	})

	it('skips image markers already present as literal draft text', async () => {
	  ;(window as unknown as { go: unknown }).go = { main: { App: {
		SaveClipboardImage: vi.fn().mockResolvedValue('/tmp/paste.png'),
		DeleteClipboardImage: vi.fn().mockResolvedValue(undefined),
	  } } }
	  const { result } = renderHook(() => useChat())
	  act(() => result.current.setText('literal [Image #1] '))
	  const literal = 'literal [Image #1] '
	  await act(async () => {
		await result.current.pasteImages([new File(['png'], 'shot.png', { type: 'image/png' })], literal.length, literal.length)
	  })
	  expect(result.current.text).toContain('[Image #2]')
	  expect(result.current.imageAttachments[0]?.label).toBe('[Image #2]')
	})

	it('keeps the draft available when starting the run fails', async () => {
	  const send = vi.fn().mockRejectedValue(new Error('model was removed'))
	  const remove = vi.fn().mockResolvedValue(undefined)
	  ;(window as unknown as { go: unknown }).go = { main: { App: {
		SaveClipboardImage: vi.fn().mockResolvedValue('/tmp/draft.png'),
		DeleteClipboardImage: remove,
		SendMessage: send,
	  } } }
	  const { result } = renderHook(() => useChat())
	  await act(async () => {
		await result.current.pasteImages([new File(['png'], 'shot.png', { type: 'image/png' })], 0, 0)
	  })

	  await act(async () => {
		result.current.send()
		await Promise.resolve()
	  })

	  expect(result.current.text).toBe('[Image #1]')
	  expect(result.current.imageAttachments).toEqual([{ label: '[Image #1]', path: '/tmp/draft.png' }])
	  expect(remove).not.toHaveBeenCalled()
	  expect(result.current.error).toContain('model was removed')
	})

	it('does not restore hidden images when the draft changes before start failure returns', async () => {
	  let rejectSend!: (reason: Error) => void
	  const send = vi.fn().mockReturnValue(new Promise<void>((_, reject) => { rejectSend = reject }))
	  const remove = vi.fn().mockResolvedValue(undefined)
	  ;(window as unknown as { go: unknown }).go = { main: { App: {
		SaveClipboardImage: vi.fn().mockResolvedValue('/tmp/old-draft.png'),
		DeleteClipboardImage: remove,
		SendMessage: send,
	  } } }
	  const { result } = renderHook(() => useChat())
	  await act(async () => {
		await result.current.pasteImages([new File(['png'], 'shot.png', { type: 'image/png' })], 0, 0)
	  })

	  act(() => result.current.send())
	  act(() => {
		emit('agent:status', { status: 'idle' })
		result.current.setText('new draft')
	  })
	  await act(async () => {
		rejectSend(new Error('start failed'))
		await Promise.resolve()
	  })

	  expect(result.current.text).toBe('new draft')
	  expect(result.current.imageAttachments).toHaveLength(0)
	  expect(remove).toHaveBeenCalledWith('/tmp/old-draft.png')
	  expect(result.current.msgs).toHaveLength(1)
	  expect(result.current.msgs[0].text).toContain('start failed')
	})

	it('deletes a draft image when its placeholder is removed', async () => {
	  const remove = vi.fn().mockResolvedValue(undefined)
	  ;(window as unknown as { go: unknown }).go = { main: { App: {
		SaveClipboardImage: vi.fn().mockResolvedValue('/tmp/draft.png'),
		DeleteClipboardImage: remove,
	  } } }
	  const { result } = renderHook(() => useChat())
	  await act(async () => {
		await result.current.pasteImages([new File(['png'], 'shot.png', { type: 'image/png' })], 0, 0)
	  })
	  act(() => result.current.setText(''))
	  await waitFor(() => expect(remove).toHaveBeenCalledWith('/tmp/draft.png'))
	  expect(result.current.imageAttachments).toHaveLength(0)
	})

	it('reports clipboard image cleanup failures', async () => {
	  const remove = vi.fn().mockRejectedValue(new Error('disk is read-only'))
	  const log = vi.spyOn(console, 'error').mockImplementation(() => undefined)
	  ;(window as unknown as { go: unknown }).go = { main: { App: {
		SaveClipboardImage: vi.fn().mockResolvedValue('/tmp/draft.png'),
		DeleteClipboardImage: remove,
	  } } }
	  const { result } = renderHook(() => useChat())
	  await act(async () => {
		await result.current.pasteImages([new File(['png'], 'shot.png', { type: 'image/png' })], 0, 0)
	  })

	  act(() => result.current.setText(''))
	  await waitFor(() => expect(result.current.error).toContain('disk is read-only'))
	  expect(log).toHaveBeenCalled()
	  log.mockRestore()
	})

	it('replaces and cleans an image when another image is pasted over its marker', async () => {
	  const remove = vi.fn().mockResolvedValue(undefined)
	  const save = vi.fn()
		.mockResolvedValueOnce('/tmp/first.png')
		.mockResolvedValueOnce('/tmp/second.png')
	  ;(window as unknown as { go: unknown }).go = { main: { App: {
		SaveClipboardImage: save,
		DeleteClipboardImage: remove,
	  } } }
	  const { result } = renderHook(() => useChat())
	  const file = new File(['png'], 'shot.png', { type: 'image/png' })
	  await act(async () => { await result.current.pasteImages([file], 0, 0) })
	  await act(async () => { await result.current.pasteImages([file], 0, '[Image #1]'.length) })

	  expect(remove).toHaveBeenCalledWith('/tmp/first.png')
	  expect(result.current.text).toBe('[Image #2]')
	  expect(result.current.imageAttachments).toEqual([{ label: '[Image #2]', path: '/tmp/second.png' }])
	})

	it('discards an in-flight paste when the session draft changes', async () => {
	  let finishSave!: (path: string) => void
	  const remove = vi.fn().mockResolvedValue(undefined)
	  const save = vi.fn().mockReturnValue(new Promise<string>((resolve) => { finishSave = resolve }))
	  ;(window as unknown as { go: unknown }).go = { main: { App: {
		SaveClipboardImage: save,
		DeleteClipboardImage: remove,
	  } } }
	  const { result } = renderHook(() => useChat())
	  let paste!: Promise<void>
	  act(() => { paste = result.current.pasteImages([new File(['png'], 'shot.png', { type: 'image/png' })], 0, 0) })
	  await waitFor(() => expect(save).toHaveBeenCalled())
	  act(() => result.current.clearMessages())
	  await act(async () => {
		finishSave('/tmp/old-session.png')
		await paste
	  })

	  expect(remove).toHaveBeenCalledWith('/tmp/old-session.png')
	  expect(result.current.text).toBe('')
	  expect(result.current.imageAttachments).toHaveLength(0)
	})

	it('rejects oversized images before reading or saving them', async () => {
	  const save = vi.fn()
	  ;(window as unknown as { go: unknown }).go = { main: { App: { SaveClipboardImage: save } } }
	  const { result } = renderHook(() => useChat())
	  const image = new File(['x'], 'huge.png', { type: 'image/png' })
	  Object.defineProperty(image, 'size', { value: 20 * 1024 * 1024 + 1 })

	  await act(async () => { await result.current.pasteImages([image], 0, 0) })
	  expect(save).not.toHaveBeenCalled()
	  expect(result.current.error).toContain('20 MiB')
	})

  it('keeps streaming compaction separate and restores the final summary after missed deltas', () => {
    const { result } = renderHook(() => useChat())
    act(() => result.current.send('/compact'))
    const event = { id: 'compact-1', status: 'started', trigger: 'manual', beforeTokens: 10000, afterTokens: 0, beforeMessages: 20, afterMessages: 0, elapsedMs: 0 }
    act(() => emit('agent:compaction', event))
    act(() => emit('agent:compaction', { ...event, status: 'delta', delta: 'partial' }))
    expect(result.current.msgs[1].compaction?.summary).toBe('partial')
    expect(result.current.msgs[2].streamText).toBe('')
    act(() => emit('agent:compaction', { ...event, status: 'completed', summary: 'complete summary', afterMessages: 6, afterTokens: 2000 }))
    act(() => emit('agent:done', { output: '', error: '' }))
    expect(result.current.msgs.find((m) => m.compaction)?.compaction?.summary).toBe('complete summary')
    expect(result.current.msgs.find((m) => m.compaction)?.compaction?.status).toBe('completed')
  })

  it('settles partial compaction when stopping and ignores late deltas', () => {
    const { result } = renderHook(() => useChat())
    act(() => result.current.send('hello'))
    const event = { id: 'compact-1', status: 'delta', trigger: 'auto', delta: 'partial', beforeTokens: 1, afterTokens: 0, beforeMessages: 10, afterMessages: 0, elapsedMs: 0 }
    act(() => emit('agent:compaction', event))
    act(() => result.current.stop())
    act(() => emit('agent:compaction', event))
    expect(result.current.msgs).toHaveLength(1)
    expect(result.current.msgs[0].compaction?.status).toBe('failed')
    expect(result.current.msgs[0].compaction?.summary).toBe('partial')
  })
  it('initializes with empty state', () => {
    const { result } = renderHook(() => useChat())
    expect(result.current.msgs).toHaveLength(0)
    expect(result.current.busy).toBe(false)
    expect(result.current.error).toBeNull()
  })

  it('sends a user message and creates a Run placeholder', () => {
    const { result } = renderHook(() => useChat())

    act(() => {
      result.current.setText('hello')
    })

    act(() => {
      result.current.send()
    })

    // New semantics: user msg + immediately seeded empty assistant Run.
    expect(result.current.msgs).toHaveLength(2)
    expect(result.current.msgs[0].role).toBe('user')
    expect(result.current.msgs[0].text).toBe('hello')
    expect(result.current.msgs[1].role).toBe('assistant')
    expect(result.current.msgs[1].streaming).toBe(true)
    expect(result.current.msgs[1].text).toBe('')
    expect(result.current.text).toBe('')
  })

  it('removes the active turn when stopped', async () => {
    const { result } = renderHook(() => useChat())

    act(() => {
      result.current.setText('stop me')
    })
    act(() => {
      result.current.send()
    })
    await waitFor(() => expect(result.current.msgs).toHaveLength(2))

    act(() => {
      result.current.stop()
    })

    expect(result.current.msgs).toHaveLength(0)
    expect(result.current.busy).toBe(true)

    act(() => {
      emit('agent:done', { output: 'Interrupted', error: 'request cancelled' })
    })

    expect(result.current.msgs).toHaveLength(0)
    expect(result.current.error).toBeNull()
  })

  it('appends deltas to the seeded Run message', async () => {
    const { result } = renderHook(() => useChat())

    act(() => {
      result.current.setText('hello')
    })
    act(() => {
      result.current.send()
    })
    await waitFor(() => expect(result.current.msgs).toHaveLength(2))

    act(() => {
      emit('agent:delta', { id: 1, delta: 'Hello', done: false })
    })
    await waitFor(() => expect(result.current.msgs[1].streamText).toBe('Hello'))
    expect(result.current.msgs[1].text).toBe('')
    expect(result.current.msgs[1].streaming).toBe(true)

    act(() => {
      emit('agent:delta', { id: 1, delta: ' world', done: false })
    })
    await waitFor(() => expect(result.current.msgs[1].streamText).toBe('Hello world'))

    act(() => {
      emit('agent:delta', { id: 1, delta: '', done: true })
    })
    await waitFor(() => expect(result.current.msgs[1].streaming).toBe(false))
  })

  it('retracts rendered and buffered response text without losing earlier tool preambles', async () => {
    const { result } = renderHook(() => useChat())
    act(() => { result.current.setText('hello') })
    act(() => { result.current.send() })
    await waitFor(() => expect(result.current.msgs).toHaveLength(2))
    act(() => { emit('agent:delta', { id: 1, delta: 'keep', done: false }) })
    await waitFor(() => expect(result.current.msgs[1].streamText).toBe('keep'))
    act(() => { emit('agent:tool_start', { id: 'c', toolName: 'read', args: '', preview: '' }) })
    act(() => { emit('agent:delta', { id: 1, delta: 'injected', done: false }) })
    await waitFor(() => expect(result.current.msgs[1].streamText).toContain('injected'))
    act(() => {
      emit('agent:delta', { id: 1, delta: ' tail', done: false })
      emit('agent:delta', { id: 1, delta: '', done: false, reset: true })
    })
    await waitFor(() => expect(result.current.msgs[1].streamText?.trim()).toBe('keep'))
  })

  it('keeps adjacent stream chunks on the same line', async () => {
    const { result } = renderHook(() => useChat())

    act(() => {
      result.current.setText('stream')
    })
    act(() => {
      result.current.send()
    })
    await waitFor(() => expect(result.current.msgs).toHaveLength(2))

    act(() => {
      emit('agent:delta', { id: 1, delta: 'checked package.json', done: false })
      emit('agent:delta', { id: 1, delta: 'reading src/App.tsx', done: false })
    })

    await waitFor(() => expect(result.current.msgs[1].streamText).toBe('checked package.jsonreading src/App.tsx'))
  })

  it('separates temporary output at tool boundaries', async () => {
    const { result } = renderHook(() => useChat())

    act(() => {
      result.current.setText('stream')
    })
    act(() => {
      result.current.send()
    })
    await waitFor(() => expect(result.current.msgs).toHaveLength(2))

    act(() => {
      emit('agent:delta', { id: 1, delta: '先检查配置', done: false })
      emit('agent:tool_start', { id: 'r1', toolName: 'read', args: '{"path":"a"}', preview: '', blocked: false })
      emit('agent:delta', { id: 1, delta: '再检查入口', done: false })
    })

    await waitFor(() => expect(result.current.msgs[1].streamText).toBe('先检查配置\n再检查入口'))
  })

  it('replaces transient stream text with final done output', async () => {
    const { result } = renderHook(() => useChat())

    act(() => {
      result.current.setText('finalize')
    })
    act(() => {
      result.current.send()
    })
    await waitFor(() => expect(result.current.msgs).toHaveLength(2))

    act(() => {
      emit('agent:delta', { id: 1, delta: 'checking files', done: false })
      emit('agent:todos', { items: [{ content: 'inspect', status: 'completed' }] })
    })
    await waitFor(() => expect(result.current.msgs[1].streamText).toBe('checking files'))
    await waitFor(() => expect(result.current.msgs[1].todos).toHaveLength(1))

    act(() => {
      emit('agent:done', { output: 'Final answer', error: '' })
    })

    await waitFor(() => expect(result.current.msgs[1].text).toBe('Final answer'))
    expect(result.current.msgs[1].streamText).toBe('')
    expect(result.current.msgs[1].todos).toBeUndefined()
    expect(result.current.msgs[1].phase).toBeUndefined()
    expect(result.current.msgs[1].tokens).toBeUndefined()
  })

  it('keeps only persistent tool metadata after a successful run', async () => {
    const { result } = renderHook(() => useChat())

    act(() => {
      result.current.setText('edit')
    })
    act(() => {
      result.current.send()
    })
    await waitFor(() => expect(result.current.msgs).toHaveLength(2))

    act(() => {
      emit('agent:tool_start', { id: 'ls1', toolName: 'ls', args: '', preview: '', blocked: false })
      emit('agent:tool_done', { id: 'ls1', toolName: 'ls', args: '', output: 'files', isError: false })
      emit('agent:tool_start', { id: 'edit1', toolName: 'edit', args: '{"path":"a.go"}', preview: '+1:change', blocked: false })
      emit('agent:tool_done', { id: 'edit1', toolName: 'edit', args: '{"path":"a.go"}', output: '[a.go#TAG]\n+1:change', isError: false })
      emit('agent:done', { output: 'Done', error: '' })
    })

    await waitFor(() => expect(result.current.msgs[1].text).toBe('Done'))
    expect(result.current.msgs[1].phase).toBe('ready')
    expect(result.current.msgs[1].steps?.map((s) => s.toolName)).toEqual(['edit'])
  })

  it('adds tool steps to the current Run', async () => {
    const { result } = renderHook(() => useChat())

    act(() => {
      result.current.setText('run tool')
    })
    act(() => {
      result.current.send()
    })
    await waitFor(() => expect(result.current.msgs).toHaveLength(2))

    act(() => {
      emit('agent:tool_start', { id: 't1', toolName: 'ls', args: '', preview: '', blocked: false })
    })
    await waitFor(() => expect(result.current.msgs[1].steps).toHaveLength(1))
    expect(result.current.msgs[1].steps![0].toolName).toBe('ls')
    expect(result.current.msgs[1].steps![0].status).toBe('running')

    act(() => {
      emit('agent:tool_done', { id: 't1', toolName: 'ls', args: '', output: 'file.txt', isError: false })
    })
    await waitFor(() => expect(result.current.msgs[1].steps![0].status).toBe('done'))
    // 非持久化工具成功后丢弃 output，与 TUI/session view 一致。
    expect(result.current.msgs[1].steps![0].output).toBe('')
  })

  it('keeps output for persistent tools and clears it for transient tools', async () => {
    const { result } = renderHook(() => useChat())

    act(() => {
      result.current.setText('do work')
    })
    act(() => {
      result.current.send()
    })
    await waitFor(() => expect(result.current.msgs).toHaveLength(2))

    act(() => {
      emit('agent:tool_start', { id: 't1', toolName: 'ls', args: '{"path":"a"}', preview: 'preview-a', blocked: false })
      emit('agent:tool_start', { id: 't2', toolName: 'shell', args: '', preview: 'preview-b', blocked: false })
    })
    await waitFor(() => expect(result.current.msgs[1].steps).toHaveLength(2))

    act(() => {
      emit('agent:tool_done', { id: 't1', toolName: 'ls', args: '', output: 'content-a', isError: false })
      emit('agent:tool_done', { id: 't2', toolName: 'shell', args: '', output: 'content-b', isError: false })
    })
    await waitFor(() => expect(result.current.msgs[1].steps![1].status).toBe('done'))

    const readStep = result.current.msgs[1].steps!.find((s) => s.toolName === 'ls')!
    const bashStep = result.current.msgs[1].steps!.find((s) => s.toolName === 'shell')!
    expect(readStep.output).toBe('')
    expect(readStep.preview).toBeUndefined()
    expect(bashStep.output).toBe('content-b')
    expect(bashStep.preview).toBe('preview-b')
  })

  it('compacts successful read tools out of the visible step list', async () => {
    const { result } = renderHook(() => useChat())

    act(() => {
      result.current.setText('inspect')
    })
    act(() => {
      result.current.send()
    })
    await waitFor(() => expect(result.current.msgs).toHaveLength(2))

    act(() => {
      emit('agent:tool_start', { id: 'r1', toolName: 'read', args: '{"path":"a"}', preview: '', blocked: false })
      emit('agent:tool_done', { id: 'r1', toolName: 'read', args: '{"path":"a"}', output: 'content', isError: false })
    })

    await act(async () => {
      await new Promise((r) => setTimeout(r, 60))
    })
    expect(result.current.msgs[1].steps).toHaveLength(0)

    act(() => {
      emit('agent:tool_done', { id: 'r2', toolName: 'read', args: '{"path":"missing"}', output: 'not found', isError: true })
    })

    await waitFor(() => expect(result.current.msgs[1].steps).toHaveLength(1))
    expect(result.current.msgs[1].steps![0].isError).toBe(true)
  })

  it('hides successful todo_write tool rows while keeping todos visible', async () => {
    const { result } = renderHook(() => useChat())

    act(() => {
      result.current.setText('plan')
    })
    act(() => {
      result.current.send()
    })
    await waitFor(() => expect(result.current.msgs).toHaveLength(2))

    act(() => {
      emit('agent:tool_start', { id: 'todo1', toolName: 'todo_write', args: '{}', preview: '', blocked: false })
      emit('agent:tool_done', { id: 'todo1', toolName: 'todo_write', args: '{}', output: 'ok', isError: false })
      emit('agent:todos', { items: [{ content: 'review', status: 'in_progress' }] })
    })

    await waitFor(() => expect(result.current.msgs[1].todos).toHaveLength(1))
    expect(result.current.msgs[1].steps).toHaveLength(0)
  })

  it('hides question tool rows', async () => {
    const { result } = renderHook(() => useChat())

    act(() => {
      result.current.setText('ask')
    })
    act(() => {
      result.current.send()
    })
    await waitFor(() => expect(result.current.msgs).toHaveLength(2))

    act(() => {
      emit('agent:tool_start', { id: 'q1', toolName: 'question', args: '{}', preview: '', blocked: false })
      emit('agent:tool_done', { id: 'q1', toolName: 'question', args: '{}', output: 'answer', isError: false })
    })

    await new Promise((r) => setTimeout(r, 10))
    expect(result.current.msgs[1].steps).toHaveLength(0)
  })

  it('extracts only local image_gen paths from output that also contains CDN URLs', async () => {
    const { result } = renderHook(() => useChat())

    act(() => {
      result.current.setText('draw')
    })
    act(() => {
      result.current.send()
    })
    await waitFor(() => expect(result.current.msgs).toHaveLength(2))

    const output = [
      'Generated images:',
      '  https://p3-aiop-sign.byteimg.com/tos-cn-i-vuqhorh59i/example.jpg?x-expires=1784486738&x-signature=test',
      '  => /tmp/nekocode_img_20260719_024538_1.jpg',
    ].join('\n')

    act(() => {
      emit('agent:tool_start', { id: 'img1', toolName: 'image_gen', args: '{}', preview: '', blocked: false })
      emit('agent:tool_done', { id: 'img1', toolName: 'image_gen', args: '{}', output, isError: false })
    })

    await waitFor(() => expect(result.current.msgs[1].images).toHaveLength(1))
    expect(result.current.msgs[1].images![0].path).toBe('/tmp/nekocode_img_20260719_024538_1.jpg')
  })

  it('uses final edit diff on success and error output on failure', async () => {
    const { result } = renderHook(() => useChat())

    act(() => {
      result.current.setText('edit file')
    })
    act(() => {
      result.current.send()
    })
    await waitFor(() => expect(result.current.msgs).toHaveLength(2))

    act(() => {
      emit('agent:tool_start', { id: 't1', toolName: 'edit', args: '', preview: '+1:diff', blocked: false })
    })
    await waitFor(() => expect(result.current.msgs[1].steps).toHaveLength(1))

    act(() => {
      emit('agent:tool_done', { id: 't1', toolName: 'edit', args: '', output: '[foo.go#TAG]\n+1:diff', isError: false })
    })
    await waitFor(() => expect(result.current.msgs[1].steps![0].status).toBe('done'))
    expect(result.current.msgs[1].steps![0].preview).toBe('[foo.go#TAG]\n+1:diff')
    expect(result.current.msgs[1].steps![0].output).toBe('')

    act(() => {
      emit('agent:tool_start', { id: 't2', toolName: 'edit', args: '', preview: '+2:diff', blocked: false })
    })
    await waitFor(() => expect(result.current.msgs[1].steps).toHaveLength(2))

    act(() => {
      emit('agent:tool_done', { id: 't2', toolName: 'edit', args: '', output: 'error: cannot apply', isError: true })
    })
    await waitFor(() => expect(result.current.msgs[1].steps![1].status).toBe('error'))
    expect(result.current.msgs[1].steps![1].preview).toBeUndefined()
    expect(result.current.msgs[1].steps![1].output).toBe('error: cannot apply')
  })

  it('ignores chat/think legacy step actions', async () => {
    const { result } = renderHook(() => useChat())

    act(() => {
      result.current.setText('hi')
    })
    act(() => {
      result.current.send()
    })
    await waitFor(() => expect(result.current.msgs).toHaveLength(2))

    act(() => {
      emit('agent:step', { action: 'chat', toolName: '', toolArgs: '', output: '' })
      emit('agent:step', { action: 'think', toolName: '', toolArgs: '', output: '' })
    })

    await new Promise((r) => setTimeout(r, 10))
    expect(result.current.msgs).toHaveLength(2)  // only user + placeholder
  })

  it('reflects busy status from agent:status', async () => {
    const { result } = renderHook(() => useChat())

    act(() => {
      emit('agent:status', { status: 'thinking' })
    })
    await waitFor(() => expect(result.current.busy).toBe(true))

    act(() => {
      emit('agent:status', { status: 'idle' })
    })
    await waitFor(() => expect(result.current.busy).toBe(false))
  })

  it('resets busy state after stopping is confirmed', async () => {
    const { result } = renderHook(() => useChat())

    act(() => {
      emit('agent:status', { status: 'running' })
    })
    await waitFor(() => expect(result.current.busy).toBe(true))

    act(() => {
      result.current.stop()
    })

    expect(result.current.busy).toBe(true)
    await act(async () => emit('agent:status', { status: 'idle' }))
    expect(result.current.busy).toBe(false)
  })

  it('surfaces done errors as a separate assistant message and finalises the Run', async () => {
    const { result } = renderHook(() => useChat())

    act(() => {
      result.current.setText('boom')
    })
    act(() => {
      result.current.send()
    })
    await waitFor(() => expect(result.current.msgs).toHaveLength(2))

    act(() => {
      emit('agent:done', { output: '', error: 'something went wrong' })
    })

    await waitFor(() => expect(result.current.msgs).toHaveLength(3))
    expect(result.current.error).toBe('something went wrong')
    // Run placeholder 第 2 条变为完成态; 第 3 条是新的错误助手消息。
    expect(result.current.msgs[1].streaming).toBe(false)
    expect(result.current.msgs[2].role).toBe('assistant')
    expect(result.current.msgs[2].text).toContain('something went wrong')
  })
  it.each(['stop', 'replace'])('settles image ownership before a delayed startup resolves after %s', async (action) => {
    let resolveSend!: () => void
    const remove = vi.fn().mockResolvedValue(undefined)
    const abort = vi.fn().mockResolvedValue(undefined)
    ;(window as unknown as { go: unknown }).go = { main: { App: {
      SendMessage: () => new Promise<void>(resolve => { resolveSend = resolve }),
      SaveClipboardImage: vi.fn().mockResolvedValue('/tmp/delayed-image.png'),
      DeleteClipboardImage: remove,
      Abort: abort,
    } } }
    const { result } = renderHook(() => useChat())
    await act(async () => result.current.pasteImages([new File(['png'], 'shot.png', { type: 'image/png' })], 0, 0))
    act(() => result.current.send())
    act(() => {
      if (action === 'stop') result.current.stop()
      else result.current.clearMessages()
    })
    if (action === 'stop') {
      expect(result.current.imageAttachments).toHaveLength(1)
      expect(result.current.text).toBe('[Image #1]')
      expect(remove).not.toHaveBeenCalled()
    } else {
      expect(result.current.imageAttachments).toHaveLength(0)
      expect(remove).toHaveBeenCalledTimes(1)
      act(() => result.current.setText('new session draft'))
    }
    await act(async () => resolveSend())
    if (action === 'stop') {
      expect(abort).toHaveBeenCalledTimes(1)
      act(() => emit('agent:status', { status: 'idle' }))
      expect(result.current.imageAttachments).toHaveLength(1)
      expect(result.current.text).toBe('[Image #1]')
      expect(remove).not.toHaveBeenCalled()
    } else {
      expect(result.current.text).toBe('new session draft')
      expect(result.current.imageAttachments).toHaveLength(0)
      expect(remove).toHaveBeenCalledTimes(1)
    }
    expect(result.current.error).toBeNull()
  })

})

it.each([false, true])('settles old cancellation after a queued run starts (new run finished: %s)', async (finished) => {
  let acknowledge!: () => void
  ;(window as unknown as { go: unknown }).go = { main: { App: {
    SendMessage: vi.fn().mockResolvedValue(undefined),
    Abort: () => new Promise<void>(resolve => { acknowledge = resolve }),
  } } }
  const { result } = renderHook(() => useChat())
  act(() => result.current.send('old demand'))
  act(() => result.current.stop())
  await act(async () => { await Promise.resolve() })
  act(() => {
    emit('agent:done', { output: '', error: 'cancelled' })
    emit('agent:status', { status: 'idle' })
    emit('agent:status', { status: 'running' })
    emit('agent:input_accepted', { queued: true, source: { kind: 'gui_queue' }, content: 'new demand' })
  })
  if (finished) act(() => {
    emit('agent:done', { output: 'new answer', error: '' })
    emit('agent:status', { status: 'idle' })
  })
  await act(async () => acknowledge())
  expect(result.current.stopping).toBe(false)
  expect(result.current.busy).toBe(!finished)
  expect(result.current.msgs.some(message => message.text === 'new demand')).toBe(true)
})

it.each([['gui', false], ['gui', true], ['gui_queue', true]] as const)('releases submission ownership after %s admission (queued: %s) and RPC acknowledgment', async (kind, queued) => {
  let acknowledge!: () => void
  const send = vi.fn(() => new Promise<void>(resolve => { acknowledge = resolve }))
  ;(window as unknown as { go: unknown }).go = { main: { App: { SendMessage: send } } }
  const { result } = renderHook(() => useChat())
  act(() => result.current.send('first'))
  act(() => emit('agent:input_accepted', { queued, source: { kind }, content: 'first' }))
  await act(async () => acknowledge())
  expect(result.current.busy).toBe(true)
  act(() => result.current.send('still running'))
  expect(send).toHaveBeenCalledTimes(1)
  act(() => emit('agent:status', { status: 'idle' }))
  act(() => result.current.send('second'))
  expect(send).toHaveBeenCalledTimes(2)
})

it.each([false, true])('replaces an unadmitted local turn after an older run finishes (RPC first: %s)', async (rpcFirst) => {
  let acknowledge!: () => void
  ;(window as unknown as { go: unknown }).go = { main: { App: {
    SendMessage: () => new Promise<void>(resolve => { acknowledge = resolve }),
  } } }
  const { result } = renderHook(() => useChat())
  act(() => result.current.send('queued local demand'))
  if (rpcFirst) await act(async () => acknowledge())
  act(() => {
    emit('agent:delta', { id: 1, delta: 'older coordinator reply', done: false })
    emit('agent:done', { output: 'older coordinator reply', error: '' })
    emit('agent:status', { status: 'idle' })
    emit('agent:status', { status: 'running' })
    emit('agent:input_accepted', { source: { kind: 'gui' }, queued: true, content: 'queued local demand' })
    emit('agent:done', { output: 'local reply', error: '' })
  })
  if (!rpcFirst) await act(async () => acknowledge())
  expect(result.current.msgs.map(message => [message.role, message.text])).toEqual([
    ['assistant', 'older coordinator reply'], ['user', 'queued local demand'], ['assistant', 'local reply'],
  ])
  expect(result.current.msgs.some(message => message.streaming)).toBe(false)
})

it('retains the older reply when the unadmitted local RPC later rejects', async () => {
  let rejectSend!: (error: Error) => void
  ;(window as unknown as { go: unknown }).go = { main: { App: {
    SendMessage: () => new Promise<void>((_, reject) => { rejectSend = reject }),
  } } }
  const { result } = renderHook(() => useChat())
  act(() => result.current.send('not accepted'))
  act(() => emit('agent:done', { output: 'older coordinator reply', error: '' }))
  await act(async () => rejectSend(new Error('queue rejected')))
  expect(result.current.msgs.some(message => message.role === 'user')).toBe(false)
  expect(result.current.msgs.filter(message => message.role === 'assistant').map(message => message.text)).toEqual([
    'older coordinator reply', 'Error: Error: queue rejected',
  ])
  expect(result.current.text).toBe('not accepted')
  expect(result.current.busy).toBe(false)
})

it('keeps an older reply when the local RPC rejects after queued admission', async () => {
  let rejectSend!: (error: Error) => void
  ;(window as unknown as { go: unknown }).go = { main: { App: {
    SendMessage: () => new Promise<void>((_, reject) => { rejectSend = reject }),
  } } }
  const { result } = renderHook(() => useChat())
  act(() => result.current.send('queued demand'))
  act(() => {
    emit('agent:done', { output: 'older coordinator reply', error: '' })
    emit('agent:input_accepted', { source: { kind: 'gui_queue' }, queued: true, content: 'queued demand' })
  })
  await act(async () => rejectSend(new Error('late RPC failure')))
  expect(result.current.msgs.some(message => message.text === 'older coordinator reply')).toBe(true)
  expect(result.current.msgs.filter(message => message.role === 'user').map(message => message.text)).toEqual(['queued demand'])
  expect(result.current.busy).toBe(true)
  act(() => emit('agent:done', { output: 'local reply', error: '' }))
  expect(result.current.msgs.some(message => message.text === 'local reply')).toBe(true)
})
