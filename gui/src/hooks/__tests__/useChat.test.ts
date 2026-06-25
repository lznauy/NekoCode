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
    await waitFor(() => expect(result.current.msgs[1].text).toBe('Hello'))
    expect(result.current.msgs[1].streaming).toBe(true)

    act(() => {
      emit('agent:delta', { id: 1, delta: ' world', done: false })
    })
    await waitFor(() => expect(result.current.msgs[1].text).toBe('Hello world'))

    act(() => {
      emit('agent:delta', { id: 1, delta: '', done: true })
    })
    await waitFor(() => expect(result.current.msgs[1].streaming).toBe(false))
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
      emit('agent:tool_start', { id: 't1', toolName: 'read', args: '{"path":"a"}', preview: 'preview-a', blocked: false })
      emit('agent:tool_start', { id: 't2', toolName: 'bash', args: '', preview: 'preview-b', blocked: false })
    })
    await waitFor(() => expect(result.current.msgs[1].steps).toHaveLength(2))

    act(() => {
      emit('agent:tool_done', { id: 't1', toolName: 'read', args: '', output: 'content-a', isError: false })
      emit('agent:tool_done', { id: 't2', toolName: 'bash', args: '', output: 'content-b', isError: false })
    })
    await waitFor(() => expect(result.current.msgs[1].steps![1].status).toBe('done'))

    const readStep = result.current.msgs[1].steps!.find((s) => s.toolName === 'read')!
    const bashStep = result.current.msgs[1].steps!.find((s) => s.toolName === 'bash')!
    expect(readStep.output).toBe('')
    expect(readStep.preview).toBeUndefined()
    expect(bashStep.output).toBe('content-b')
    expect(bashStep.preview).toBe('preview-b')
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
