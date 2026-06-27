import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useChat } from '../useChat'

const listeners: Record<string, Array<(...args: unknown[]) => void>> = {}

function emit(event: string, data: unknown): void {
  listeners[event]?.forEach((cb) => cb(data))
}

 beforeEach(() => {
  Object.keys(listeners).forEach((k) => delete listeners[k])

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
    // 非持久化工具成功后丢弃 output，与 TUI/sessionview 一致。
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
      emit('agent:tool_start', { id: 't2', toolName: 'bash', args: '', preview: 'preview-b', blocked: false })
    })
    await waitFor(() => expect(result.current.msgs[1].steps).toHaveLength(2))

    act(() => {
      emit('agent:tool_done', { id: 't1', toolName: 'ls', args: '', output: 'content-a', isError: false })
      emit('agent:tool_done', { id: 't2', toolName: 'bash', args: '', output: 'content-b', isError: false })
    })
    await waitFor(() => expect(result.current.msgs[1].steps![1].status).toBe('done'))

    const readStep = result.current.msgs[1].steps!.find((s) => s.toolName === 'ls')!
    const bashStep = result.current.msgs[1].steps!.find((s) => s.toolName === 'bash')!
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
