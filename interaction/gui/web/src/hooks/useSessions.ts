import { useCallback, useEffect, useRef, useState } from 'react'
import {
  safeDeleteSession,
  safeEventsOn,
  safeListSessions,
  safeLoadSession,
  safeNewSession,
} from '../lib/wails'
import { genId } from '../lib/id'
import type { Msg, ToolStep } from '../types/events'
import type { runtime } from '../../wailsjs/go/models'
type SessionMeta = runtime.SessionMeta
type DisplayMessage = runtime.DisplayMessage

export interface SessionChangeEvent {
  attachmentDraft?: boolean
  id?: string
  reason?: string
  changedId?: string
	currentChanged?: boolean
  messages?: DisplayMessage[]
}

export function shouldReloadSessionMessages(change: SessionChangeEvent): boolean {
  if (change.attachmentDraft) return false
	return change.reason !== 'delete' || change.currentChanged === true
}

export function shouldHydrateEmptySession(currentId: string | null, attachmentDraftId: string | null): boolean {
	return Boolean(currentId) && currentId !== attachmentDraftId
}

export interface UseSessionsReturn {
  sessions: SessionMeta[]
  currentId: string | null
  loading: boolean
  error: string | null
  refresh: () => Promise<void>
  createSession: () => Promise<SessionMeta | null>
  switchSession: (id: string) => Promise<Msg[] | null>
  deleteSession: (id: string) => Promise<{ sessions: SessionMeta[]; deleted: boolean }>
}

export function useSessions(): UseSessionsReturn {
  const [sessions, setSessions] = useState<SessionMeta[]>([])
  const [currentId, setCurrentId] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const initializedRef = useRef(false)
  const syncGenerationRef = useRef(0)
  const operationRef = useRef(0)
  const pendingOperationRef = useRef<{ reason: string; id: string } | null>(null)

  const refresh = useCallback(async () => {
    const generation = ++syncGenerationRef.current
    setLoading(true)
    setError(null)
    try {
      const list = normalizeSessions(await safeListSessions())
      if (generation !== syncGenerationRef.current) return
      setSessions(list)
      // The saved list does not include an active, unsaved draft. Only a
      // session event or an explicit operation may clear that identity.
      if (list.length > 0) {
        setCurrentId((prev) => {
          if (initializedRef.current) return prev
          initializedRef.current = true
          return list[0].id
        })
      }
    } catch (err) {
      if (generation === syncGenerationRef.current) setError(String(err))
    } finally {
      if (generation === syncGenerationRef.current) setLoading(false)
    }
  }, [])

  useEffect(() => {
    refresh()
  }, [refresh])

  useEffect(() => {
    return safeEventsOn('session:changed', (event: unknown) => {
      const id = typeof (event as { id?: unknown })?.id === 'string'
        ? (event as { id: string }).id
        : ''
      const generation = ++syncGenerationRef.current
      operationRef.current++
      const change = event as SessionChangeEvent
      const pending = pendingOperationRef.current
      // An own committed event supersedes the returned snapshot, but its
      // finalization error still belongs here. Other changes invalidate both.
      if (!pending || change.reason !== pending.reason || (change.changedId ?? change.id) !== pending.id) {
        pendingOperationRef.current = null
      }
      initializedRef.current = true
      setCurrentId(id || null)
      void safeListSessions().then((list) => {
        if (generation !== syncGenerationRef.current) return
        setSessions(normalizeSessions(list))
      }).catch((err) => {
        if (generation === syncGenerationRef.current) setError(String(err))
      }).finally(() => {
        if (generation === syncGenerationRef.current) setLoading(false)
      })
    })
  }, [])

  const createSession = useCallback(async () => {
    const operation = ++operationRef.current
    const pending = { reason: 'new', id: '' }
    pendingOperationRef.current = pending
    setError(null)
    try {
      const meta = await safeNewSession()
      if (!meta || operation !== operationRef.current) return null
      setCurrentId(meta.id)
      return meta
    } catch (err) {
      if (pendingOperationRef.current === pending) setError(String(err))
      return null
    } finally {
      if (pendingOperationRef.current === pending) pendingOperationRef.current = null
    }
  }, [])

  const switchSession = useCallback(async (id: string) => {
    const operation = ++operationRef.current
    const pending = { reason: 'resume', id }
    pendingOperationRef.current = pending
    setError(null)
    try {
      const raw = await safeLoadSession(id)
      if (!raw || operation !== operationRef.current) return null
      const msgs = raw.map(mapDisplayMessage)
      setCurrentId(id)
      return msgs
    } catch (err) {
      if (pendingOperationRef.current === pending) setError(String(err))
      return null
    } finally {
      if (pendingOperationRef.current === pending) pendingOperationRef.current = null
    }
  }, [])

  const deleteSession = useCallback(async (id: string): Promise<{ sessions: SessionMeta[]; deleted: boolean }> => {
    const operation = ++operationRef.current
    const pending = { reason: 'delete', id }
    pendingOperationRef.current = pending
    setError(null)
    try {
      await safeDeleteSession(id)
      const list = normalizeSessions(await safeListSessions())
      if (operation === operationRef.current) {
        setSessions(list)
        setCurrentId(current => current === id ? null : current)
      }
      return { sessions: list, deleted: true }
    } catch (err) {
      if (pendingOperationRef.current === pending) setError(String(err))
      try {
        const list = normalizeSessions(await safeListSessions())
        const deleted = !list.some((session) => session.id === id)
        if (operation === operationRef.current) {
          setSessions(list)
          if (deleted) setCurrentId(current => current === id ? null : current)
        }
        return { sessions: list, deleted }
      } catch {
        return { sessions, deleted: false }
      }
    } finally {
      if (pendingOperationRef.current === pending) pendingOperationRef.current = null
    }
  }, [currentId, sessions])

  return {
    sessions,
    currentId,
    loading,
    error,
    refresh,
    createSession,
    switchSession,
    deleteSession,
  }
}

function normalizeSessions(list: SessionMeta[] | null | undefined): SessionMeta[] {
  return Array.isArray(list) ? list : []
}

// mapDisplayMessage 将服务端 DisplayMessage 转换为前端 Msg。
// blocks 映射为 msg.steps，由 RunCard/ActivityRow/UnifiedDiff 渲染。
// images 映射为 msg.images，由 ImageGrid 渲染。
export function mapDisplayMessage(m: DisplayMessage): Msg {
  const role = m.role as Msg['role']
  const text = m.content ?? ''
  const steps = buildStepsFromBlocks(m.blocks)
  const images = m.images?.length
    ? m.images.map((i) => ({
        path: i.path,
        url: i.url || undefined,
        width: i.width ?? 0,
        height: i.height ?? 0,
      }))
    : undefined
  return {
    id: genId(),
    role: role === 'user' || role === 'assistant' || role === 'tool' ? role : 'assistant',
    text,
		reasoning: m.reasoning || undefined,
		reasoningDone: m.reasoning ? true : undefined,
    streaming: false,
    steps: steps.length > 0 ? steps : undefined,
    images,
  }
}

function buildStepsFromBlocks(blocks: DisplayMessage['blocks']): ToolStep[] {
  if (!blocks?.length) return []
  return blocks.map((b) => {
    const isError = !!b.isError
    return {
      id: genId(),
      toolName: b.toolName,
      // args 来自 ToolCall.Arguments (原始 JSON), 是命令/参数的权威来源;
      // 只有 args 缺失时 (旧 session) 才回退到从 output 抽取 edit/write 路径。
      args: b.args || extractFilePath(b.content),
      output: b.content,
      status: isError ? 'error' as const : 'done' as const,
      isError,
      collapsed: shouldCollapseLoadedTool(b.toolName),
    }
  })
}

// extractFilePath 从 edit/write 工具的 output 首行 [PATH#TAG] 中提取文件路径。
function extractFilePath(content: string): string {
  if (!content || !content.startsWith('[')) return ''
  const nl = content.indexOf('\n')
  const header = nl === -1 ? content : content.slice(0, nl)
  if (!header.endsWith(']')) return ''
  const inner = header.slice(1, -1)
  const h = inner.lastIndexOf('#')
  return h > 0 ? inner.slice(0, h) : inner
}

function shouldCollapseLoadedTool(name: string): boolean {
  return !(name === 'edit' || name === 'shell')
}
