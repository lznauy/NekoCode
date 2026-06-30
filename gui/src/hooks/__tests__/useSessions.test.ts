import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useSessions } from '../useSessions'
import type { SessionMeta } from '../../types/session'

const mockSafeListSessions = vi.fn<() => Promise<SessionMeta[] | null>>()
const mockSafeNewSession = vi.fn<() => Promise<SessionMeta | null>>()
const mockSafeLoadSession = vi.fn()
const mockSafeDeleteSession = vi.fn<(_: string) => Promise<void>>()

vi.mock('../../lib/wails', () => ({
  safeListSessions: () => mockSafeListSessions(),
  safeNewSession: () => mockSafeNewSession(),
  safeLoadSession: (id: string) => mockSafeLoadSession(id),
  safeDeleteSession: (id: string) => mockSafeDeleteSession(id),
}))

const meta = (id: string): SessionMeta => ({
  id,
  cwd: '/repo',
  created_at: 1,
  updated_at: 1,
  msg_count: 1,
})

describe('useSessions', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockSafeListSessions.mockResolvedValue([])
    mockSafeNewSession.mockResolvedValue(meta('draft'))
    mockSafeLoadSession.mockResolvedValue([])
    mockSafeDeleteSession.mockResolvedValue()
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

  it('does not insert a new draft session into the history list', async () => {
    const { result } = renderHook(() => useSessions())
    await waitFor(() => expect(result.current.loading).toBe(false))

    await act(async () => {
      const created = await result.current.createSession()
      expect(created?.id).toBe('draft')
    })

    expect(result.current.sessions).toEqual([])
    expect(result.current.currentId).toBeNull()
  })

  it('clears the current selection after deleting the last persisted session', async () => {
    mockSafeListSessions
      .mockResolvedValueOnce([meta('one')])
      .mockResolvedValueOnce([])
    const { result } = renderHook(() => useSessions())

    await waitFor(() => expect(result.current.currentId).toBe('one'))

    await act(async () => {
      const remaining = await result.current.deleteSession('one')
      expect(remaining).toEqual([])
    })

    expect(result.current.sessions).toEqual([])
    expect(result.current.currentId).toBeNull()
  })

  it('restores failed tool blocks as error steps', async () => {
    mockSafeListSessions.mockResolvedValueOnce([meta('one')])
    mockSafeLoadSession.mockResolvedValueOnce([
      {
        Role: 'assistant',
        Content: '',
        Blocks: [
          {
            ToolName: 'bash',
            Args: '{"command":"false"}',
            Content: 'command failed: exit status 1',
            IsError: true,
          },
        ],
        Images: null,
      },
    ])
    const { result } = renderHook(() => useSessions())

    await waitFor(() => expect(result.current.currentId).toBe('one'))

    const loaded = await act(async () => result.current.switchSession('one'))

    expect(loaded?.[0].steps?.[0]).toMatchObject({
      toolName: 'bash',
      status: 'error',
      isError: true,
    })
  })

  it('loads edit and bash blocks expanded while keeping write collapsed', async () => {
    mockSafeListSessions.mockResolvedValueOnce([meta('one')])
    mockSafeLoadSession.mockResolvedValueOnce([
      {
        Role: 'assistant',
        Content: '',
        Blocks: [
          {
            ToolName: 'edit',
            Args: '{"file_path":"/repo/app.ts"}',
            Content: 'diff',
            IsError: false,
          },
          {
            ToolName: 'bash',
            Args: '{"command":"npm test"}',
            Content: 'ok',
            IsError: false,
          },
          {
            ToolName: 'write',
            Args: '{"file_path":"/repo/out.txt"}',
            Content: 'written',
            IsError: false,
          },
        ],
        Images: null,
      },
    ])
    const { result } = renderHook(() => useSessions())

    await waitFor(() => expect(result.current.currentId).toBe('one'))

    const loaded = await act(async () => result.current.switchSession('one'))
    const steps = loaded?.[0].steps ?? []

    expect(steps.map((s) => ({ toolName: s.toolName, collapsed: s.collapsed }))).toEqual([
      { toolName: 'edit', collapsed: false },
      { toolName: 'bash', collapsed: false },
      { toolName: 'write', collapsed: true },
    ])
  })
})
