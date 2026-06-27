import { useCallback, useEffect, useRef, useState } from 'react'
import {
  safeDeleteSession,
  safeListSessions,
  safeLoadSession,
  safeNewSession,
} from '../lib/wails'
import { genId } from '../lib/id'
import type { Msg, ToolStep } from '../types/events'
import type { DisplayMessage, SessionMeta } from '../types/session'

export interface UseSessionsReturn {
  sessions: SessionMeta[]
  currentId: string | null
  loading: boolean
  error: string | null
  refresh: () => Promise<void>
  createSession: () => Promise<SessionMeta | null>
  switchSession: (id: string) => Promise<Msg[] | null>
  deleteSession: (id: string) => Promise<SessionMeta[]>
}

export function useSessions(): UseSessionsReturn {
  const [sessions, setSessions] = useState<SessionMeta[]>([])
  const [currentId, setCurrentId] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const initializedRef = useRef(false)

  const refresh = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const list = normalizeSessions(await safeListSessions())
      setSessions(list)
      if (list.length === 0) {
        setCurrentId(null)
      } else {
        setCurrentId((prev) => {
          if (initializedRef.current && prev) return prev
          initializedRef.current = true
          return list[0].id
        })
      }
    } catch (err) {
      setError(String(err))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    refresh()
  }, [refresh])

  const createSession = useCallback(async () => {
    setError(null)
    try {
      const meta = await safeNewSession()
      if (!meta) return null
      setCurrentId(null)
      return meta
    } catch (err) {
      setError(String(err))
      return null
    }
  }, [])

  const switchSession = useCallback(async (id: string) => {
    setError(null)
    try {
      const raw = await safeLoadSession(id)
      if (!raw) return null
      const msgs = raw.map(mapDisplayMessage)
      setCurrentId(id)
      return msgs
    } catch (err) {
      setError(String(err))
      return null
    }
  }, [])

  const deleteSession = useCallback(async (id: string): Promise<SessionMeta[]> => {
    setError(null)
    try {
      await safeDeleteSession(id)
      const list = normalizeSessions(await safeListSessions())
      setSessions(list)
      if (currentId === id) {
        setCurrentId(list.length > 0 ? list[0].id : null)
      }
      return list
    } catch (err) {
      setError(String(err))
      return sessions
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
// Blocks（edit/write/bash）映射为 msg.steps，由 RunCard/ActivityRow/EditDiff 渲染。
// Images 映射为 msg.images，由 ImageGrid 渲染。
function mapDisplayMessage(m: DisplayMessage): Msg {
  const role = m.Role as Msg['role']
  const text = m.Content ?? ''
  const steps = buildStepsFromBlocks(m.Blocks)
  const images = m.Images?.length
    ? m.Images.map((i) => ({
        path: i.Path,
        url: i.URL || undefined,
        width: i.Width,
        height: i.Height,
      }))
    : undefined
  return {
    id: genId(),
    role: role === 'user' || role === 'assistant' || role === 'tool' ? role : 'assistant',
    text,
    streaming: false,
    steps: steps.length > 0 ? steps : undefined,
    images,
  }
}

function buildStepsFromBlocks(blocks: DisplayMessage['Blocks']): ToolStep[] {
  if (!blocks?.length) return []
  return blocks.map((b) => {
    const isError = !!b.IsError
    return {
      id: genId(),
      toolName: b.ToolName,
      // Args 来自 ToolCall.Arguments (原始 JSON), 是命令/参数的权威来源;
      // 只有 Args 缺失时 (旧 session) 才回退到从 output 抽取 edit/write 路径。
      args: b.Args || extractFilePath(b.Content),
      output: b.Content,
      status: isError ? 'error' as const : 'done' as const,
      isError,
      collapsed: persistentTool(b.ToolName),
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

function persistentTool(name: string): boolean {
  return name === 'edit' || name === 'write' || name === 'bash'
}
