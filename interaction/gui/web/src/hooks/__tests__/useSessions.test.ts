import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { shouldHydrateEmptySession, shouldReloadSessionMessages, useSessions } from '../useSessions'
import type { runtime } from '../../../wailsjs/go/models'
type SessionMeta = runtime.SessionMeta

const mockSafeListSessions = vi.fn<() => Promise<SessionMeta[] | null>>()
const mockSafeNewSession = vi.fn<() => Promise<SessionMeta | null>>()
const mockSafeLoadSession = vi.fn()
const mockSafeDeleteSession = vi.fn<(_: string) => Promise<void>>()
let sessionChanged: ((event: unknown) => void) | undefined

vi.mock('../../lib/wails', () => ({
  safeEventsOn: (event: string, callback: (payload: unknown) => void) => {
    if (event === 'session:changed') sessionChanged = callback
    return () => {}
  },
  safeListSessions: () => mockSafeListSessions(),
  safeNewSession: () => mockSafeNewSession(),
  safeLoadSession: (id: string) => mockSafeLoadSession(id),
  safeDeleteSession: (id: string) => mockSafeDeleteSession(id),
}))

const meta = (id: string): SessionMeta => ({
  id,
  cwd: '/repo',
  createdAt: 1,
  updatedAt: 1,
  msgCount: 1,
  convertValues: () => ({}),
} as SessionMeta)

describe('useSessions', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    sessionChanged = undefined
    mockSafeListSessions.mockResolvedValue([])
    mockSafeNewSession.mockResolvedValue(meta('draft'))
    mockSafeLoadSession.mockResolvedValue([])
    mockSafeDeleteSession.mockResolvedValue()
  })

  it('does not reload current messages when another session is deleted', () => {
	expect(shouldReloadSessionMessages({
	  id: 'current', reason: 'delete', changedId: 'other', currentChanged: false, messages: [],
	})).toBe(false)
	expect(shouldReloadSessionMessages({
	  id: '', reason: 'delete', changedId: 'current', currentChanged: true, messages: [],
	})).toBe(true)
	expect(shouldReloadSessionMessages({
	  id: '', reason: 'delete', changedId: 'history', currentChanged: false, messages: [],
	})).toBe(false)
  })

  it('does not hydrate an attachment-created draft session', () => {
	expect(shouldHydrateEmptySession('draft', 'draft')).toBe(false)
	expect(shouldHydrateEmptySession('normal', null)).toBe(true)
  })

  it('keeps an empty persisted list empty', async () => {
    const { result } = renderHook(() => useSessions())

    await waitFor(() => expect(result.current.loading).toBe(false))

    expect(result.current.sessions).toEqual([])
    expect(result.current.currentId).toBeNull()
  })

  it('treats null session lists from Wails as empty arrays', async () => {
    mockSafeListSessions.mockResolvedValue(null)
    const { result } = renderHook(() => useSessions())

    await waitFor(() => expect(result.current.loading).toBe(false))

    expect(result.current.sessions).toEqual([])
    expect(result.current.currentId).toBeNull()
  })

  it('selects a new draft without inserting it into the history list', async () => {
    const { result } = renderHook(() => useSessions())
    await waitFor(() => expect(result.current.loading).toBe(false))

    await act(async () => {
      const created = await result.current.createSession()
      expect(created?.id).toBe('draft')
    })

    expect(result.current.sessions).toEqual([])
    expect(result.current.currentId).toBe('draft')
  })

  it('clears the current selection after deleting the last persisted session', async () => {
    mockSafeListSessions
      .mockResolvedValueOnce([meta('one')])
      .mockResolvedValueOnce([])
    const { result } = renderHook(() => useSessions())

    await waitFor(() => expect(result.current.currentId).toBe('one'))

    await act(async () => {
	  const resultValue = await result.current.deleteSession('one')
	  expect(resultValue).toEqual({ sessions: [], deleted: true })
    })

    expect(result.current.sessions).toEqual([])
    expect(result.current.currentId).toBeNull()
  })

  it('reports a failed deletion without claiming the session was removed', async () => {
    mockSafeListSessions.mockResolvedValue([meta('one')])
    mockSafeDeleteSession.mockRejectedValue(new Error('delete failed'))
    const { result } = renderHook(() => useSessions())
    await waitFor(() => expect(result.current.currentId).toBe('one'))

    let deletion!: { sessions: SessionMeta[]; deleted: boolean }
    await act(async () => {
      deletion = await result.current.deleteSession('one')
    })

    expect(deletion.deleted).toBe(false)
    expect(deletion.sessions.map((session) => session.id)).toEqual(['one'])
    expect(result.current.currentId).toBe('one')
  })

  it('synchronizes the current session from runtime events', async () => {
    mockSafeListSessions
      .mockResolvedValueOnce([meta('one')])
      .mockResolvedValueOnce([meta('two'), meta('one')])
    const { result } = renderHook(() => useSessions())

    await waitFor(() => expect(result.current.currentId).toBe('one'))

    act(() => {
      sessionChanged?.({ id: 'two' })
    })

    await waitFor(() => expect(result.current.currentId).toBe('two'))
    expect(result.current.sessions.map((session) => session.id)).toEqual(['two', 'one'])
  })

  it('ignores stale session list results from older runtime events', async () => {
    mockSafeListSessions.mockResolvedValueOnce([meta('one')])
    const { result } = renderHook(() => useSessions())
    await waitFor(() => expect(result.current.currentId).toBe('one'))

    let resolveOlder!: (sessions: SessionMeta[]) => void
    let resolveLatest!: (sessions: SessionMeta[]) => void
    mockSafeListSessions
      .mockImplementationOnce(() => new Promise((resolve) => { resolveOlder = resolve }))
      .mockImplementationOnce(() => new Promise((resolve) => { resolveLatest = resolve }))

    act(() => sessionChanged?.({ id: 'two' }))
    act(() => sessionChanged?.({ id: 'three' }))
    expect(result.current.currentId).toBe('three')

    await act(async () => resolveLatest([meta('three')]))
    await waitFor(() => expect(result.current.sessions.map((session) => session.id)).toEqual(['three']))
    await act(async () => resolveOlder([meta('two')]))

    expect(result.current.currentId).toBe('three')
    expect(result.current.sessions.map((session) => session.id)).toEqual(['three'])
  })

  it('preserves an authoritative empty current session across refreshes', async () => {
    mockSafeListSessions.mockResolvedValue([meta('one')])
    const { result } = renderHook(() => useSessions())
    await waitFor(() => expect(result.current.currentId).toBe('one'))

    act(() => sessionChanged?.({ id: '' }))
    await waitFor(() => expect(result.current.currentId).toBeNull())
    await act(async () => {
      await result.current.refresh()
    })

    expect(result.current.sessions.map((session) => session.id)).toEqual(['one'])
    expect(result.current.currentId).toBeNull()
  })

  it('restores failed tool blocks as error steps', async () => {
    mockSafeListSessions.mockResolvedValueOnce([meta('one')])
    mockSafeLoadSession.mockResolvedValueOnce([
      {
        role: 'assistant',
        content: '',
        blocks: [
          {
            toolName: 'shell',
            args: '{"command":"false"}',
            content: 'command failed: exit status 1',
            isError: true,
          },
        ],
        images: null,
      },
    ])
    const { result } = renderHook(() => useSessions())

    await waitFor(() => expect(result.current.currentId).toBe('one'))

    const loaded = await act(async () => result.current.switchSession('one'))

    expect(loaded?.[0].steps?.[0]).toMatchObject({
      toolName: 'shell',
      status: 'error',
      isError: true,
    })
  })

  it('restores persisted assistant reasoning as completed thinking', async () => {
    mockSafeListSessions.mockResolvedValueOnce([meta('one')])
    mockSafeLoadSession.mockResolvedValueOnce([{
      role: 'assistant',
      content: 'done',
      reasoning: 'inspect repository',
      blocks: null,
      images: null,
    }])
    const { result } = renderHook(() => useSessions())

    await waitFor(() => expect(result.current.currentId).toBe('one'))
    const loaded = await act(async () => result.current.switchSession('one'))

    expect(loaded?.[0]).toMatchObject({
      reasoning: 'inspect repository',
      reasoningDone: true,
    })
  })

  it('loads edit and bash blocks expanded while keeping write collapsed', async () => {
    mockSafeListSessions.mockResolvedValueOnce([meta('one')])
    mockSafeLoadSession.mockResolvedValueOnce([
      {
        role: 'assistant',
        content: '',
        blocks: [
          {
            toolName: 'edit',
            args: '{"file_path":"/repo/app.ts"}',
            content: 'diff',
            isError: false,
          },
          {
            toolName: 'shell',
            args: '{"command":"npm test"}',
            content: 'ok',
            isError: false,
          },
          {
            toolName: 'write',
            args: '{"file_path":"/repo/out.txt"}',
            content: 'written',
            isError: false,
          },
        ],
        images: null,
      },
    ])
    const { result } = renderHook(() => useSessions())

    await waitFor(() => expect(result.current.currentId).toBe('one'))

    const loaded = await act(async () => result.current.switchSession('one'))
    const steps = loaded?.[0].steps ?? []

    expect(steps.map((s) => ({ toolName: s.toolName, collapsed: s.collapsed }))).toEqual([
      { toolName: 'edit', collapsed: false },
      { toolName: 'shell', collapsed: false },
      { toolName: 'write', collapsed: true },
    ])
  })
})
