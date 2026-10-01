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
    expect(result.current.busy).toBe(false)

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

  it('stops and resets busy state', async () => {
    const { result } = renderHook(() => useChat())

    act(() => {
      emit('agent:status', { status: 'running' })
    })
    await waitFor(() => expect(result.current.busy).toBe(true))

    act(() => {
      result.current.stop()
    })

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
})
