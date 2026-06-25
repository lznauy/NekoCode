import { useCallback, useRef, useState } from 'react'
import { genId } from '../lib/id'
import { safeAbort, safeSendMessage } from '../lib/wails'
import { useWailsEvents } from './useWailsEvents'
import type {
  AgentPhase,
  MetricsPayload,
  Msg,
  SubAgent,
  TodoItem,
  ToolStep,
} from '../types/events'
import type { UIImageRef } from '../types/events'

export interface UseChatReturn {
  msgs: Msg[]
  text: string
  setText: (text: string) => void
  busy: boolean
  error: string | null
  send: () => void
  stop: () => void
  toggleStep: (stepId: string) => void
  setMessages: (msgs: Msg[]) => void
  clearMessages: () => void
}

const emptyRunMsg = (id: string): Msg => ({
  id,
  role: 'assistant',
  text: '',
  streaming: true,
  phase: 'thinking' as AgentPhase,
  tokens: { prompt: 0, completion: 0 },
  steps: [],
  reasoning: '',
  reasoningDone: false,
  todos: [],
  subagents: [],
  elapsedMs: 0,
  compactCount: 0,
})

export function useChat(): UseChatReturn {
  const [msgs, setMsgs] = useState<Msg[]>([])
  const [text, setText] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const sidRef = useRef<string | null>(null)
  const sendingRef = useRef(false)
  const abortedRef = useRef(false)

  // 顺着 phase 切换更新 UI 状态。
  const onPhase = useCallback((e: { phase: AgentPhase }) => {
    setMsgs((prev) => upsert(prev, sidRef.current, (m) => ({ ...m, phase: e.phase })))
  }, [])

  const onDelta = useCallback((e: { id: number; delta: string; done: boolean }) => {
    const sid = sidRef.current
    if (!sid) return
    setMsgs((prev) => upsert(prev, sid, (m) => ({
      ...m,
      text: m.text + e.delta,
      streaming: !e.done,
    })))
  }, [])

  const onReasoning = useCallback((e: { delta: string; done: boolean }) => {
    const sid = sidRef.current
    if (!sid) return
    setMsgs((prev) => upsert(prev, sid, (m) => ({
      ...m,
      reasoning: m.reasoning + e.delta,
      reasoningDone: e.done,
    })))
  }, [])

  const onToolStart = useCallback((e: {
    id: string
    toolName: string
    args: string
    preview: string
    blocked?: boolean
    reason?: string
  }) => {
    const sid = sidRef.current
    if (!sid) return
    const step: ToolStep = {
      id: e.id,
      toolName: e.toolName,
      args: e.args,
      preview: e.preview,
      status: e.blocked ? 'blocked' : 'running',
      output: e.reason,
      isError: !!e.blocked,
      collapsed: false,
    }
    setMsgs((prev) => upsert(prev, sid, (m) => ({
      ...m,
      steps: [...(m.steps ?? []), step],
    })))
  }, [])

  const onToolPreview = useCallback((e: { toolName: string; preview: string }) => {
    const sid = sidRef.current
    if (!sid) return
    // FIFO 匹配: 找第一个 running 且同 toolName 的 step, 替换其 preview。
    setMsgs((prev) => upsert(prev, sid, (m) => {
      const steps = [...(m.steps ?? [])]
      for (let i = steps.length - 1; i >= 0; i--) {
        if (steps[i].toolName === e.toolName && steps[i].status === 'running') {
          steps[i] = { ...steps[i], preview: e.preview }
          return { ...m, steps }
        }
      }
      return m
    }))
  }, [])

  const onToolDone = useCallback((e: {
    id: string
    toolName: string
    args: string
    output: string
    isError: boolean
  }) => {
    const sid = sidRef.current
    if (!sid) return
    setMsgs((prev) => upsert(prev, sid, (m) => {
      const steps = [...(m.steps ?? [])]
      const byId = e.id ? steps.findIndex((s) => s.id === e.id && !terminalStep(s)) : -1
      const idx = byId !== -1 ? byId : steps.findIndex((s) => s.toolName === e.toolName && !terminalStep(s))
      if (idx === -1) return m
      const target = steps[idx]
      const isPersistent = persistentTool(e.toolName)
      const isEdit = e.toolName === 'edit'
      let output = e.output
      let preview = target.preview

      if (isEdit) {
        // 与 TUI finishToolBlock 一致：edit 成功时用最终输出替换运行时 preview，
        // 保证 relocated/rebased edits 展示准确提交 diff。
        const isRevert = output.includes('Reverted to pre-edit state')
        if (e.isError || isRevert || !output.startsWith('[')) {
          preview = undefined
        } else {
          preview = output
          output = ''
        }
      } else if (!isPersistent && !e.isError) {
        // 非持久化工具在成功后丢弃中间 preview/output，与 sessionview 只保留 edit/bash/write 一致。
        output = ''
        preview = undefined
      }

      steps[idx] = {
        ...target,
        output,
        preview,
        isError: e.isError,
        status: e.isError ? 'error' : 'done',
        collapsed: isPersistent,
      }
      return {
        ...m,
        steps,
        // image_gen 完成时立刻把图片路径注入 msg.images，不依赖 session 重新加载。
        images: e.toolName === 'image_gen' && !e.isError
          ? mergeImageRefs(m.images, parseImageOutput(e.output))
          : m.images,
      }
    }))
  }, [])

  const onSubAgentStart = useCallback((e: { id: string; subType: string; colorIdx: number }) => {
    const sid = sidRef.current
    if (!sid) return
    setMsgs((prev) => upsert(prev, sid, (m) => ({
      ...m,
      subagents: [...(m.subagents ?? []), e as SubAgent],
    })))
  }, [])

  const onSubAgentEnd = useCallback((e: { id: string }) => {
    const sid = sidRef.current
    if (!sid) return
    setMsgs((prev) => upsert(prev, sid, (m) => ({
      ...m,
      subagents: (m.subagents ?? []).filter((s) => s.id !== e.id),
    })))
  }, [])

  const onTodos = useCallback((e: { items: TodoItem[] }) => {
    const sid = sidRef.current
    if (!sid) return
    setMsgs((prev) => upsert(prev, sid, (m) => ({ ...m, todos: e.items ?? [] })))
  }, [])

  const onMetrics = useCallback((e: MetricsPayload) => {
    const sid = sidRef.current
    if (!sid) return
    setMsgs((prev) => upsert(prev, sid, (m) => ({
      ...m,
      tokens: { prompt: e.prompt, completion: e.completion },
      elapsedMs: e.elapsedMs,
      compactCount: e.compactCount,
    })))
  }, [])

  const onStep = useCallback((e: { action: string; toolName: string; output: string }) => {
    // 兜底: chat think 等不分发 action 的最终文本,
    // 主要回显到对应当前 assistant msg 的 text (按 `phase` 流程已基本覆盖, 此处空实现以保留接入点)
    void e
  }, [])

  const onDone = useCallback((e: { error: string }) => {
    const sid = sidRef.current
    if (e.error) {
      setError(e.error)
      setMsgs((prev) => [
        ...prev,
        { id: genId(), role: 'assistant' as const, text: 'Error: ' + e.error, streaming: false },
      ])
    }
    if (sid) {
      setMsgs((prev) => prev.map((m) => (m.id === sid ? { ...m, streaming: false, phase: 'ready' } : m)))
    }
    sidRef.current = null
    sendingRef.current = false
  }, [])

  const onStatus = useCallback((e: { status: string }) => {
    if (abortedRef.current) return
    setBusy(e.status !== 'idle')
  }, [])

  useWailsEvents({
    onDelta,
    onReasoning,
    onPhase,
    onToolStart,
    onToolPreview,
    onToolDone,
    onSubAgentStart,
    onSubAgentEnd,
    onTodos,
    onMetrics,
    onStep,
    onDone,
    onStatus,
  })

  const send = useCallback(() => {
    const t = text.trim()
    if (!t || busy || sendingRef.current) return

    sendingRef.current = true
    abortedRef.current = false
    setError(null)
    setMsgs((prev) => [...prev, { id: genId(), role: 'user' as const, text: t, streaming: false }])
    setText('')
    const sid = genId()
    sidRef.current = sid
    setMsgs((prev) => [...prev, emptyRunMsg(sid)])

    safeSendMessage(t).catch((err: unknown) => {
      const errStr = String(err)
      setError(errStr)
      setMsgs((prev) => [
        ...prev,
        { id: genId(), role: 'assistant' as const, text: 'Error: ' + errStr, streaming: false },
      ])
      setBusy(false)
      sidRef.current = null
      sendingRef.current = false
    })
  }, [text, busy])

  const stop = useCallback(() => {
    abortedRef.current = true
    safeAbort()
    const sid = sidRef.current
    if (sid) {
      setMsgs((prev) => prev.map((m) => (m.id === sid ? { ...m, streaming: false, phase: 'ready' } : m)))
    }
    sidRef.current = null
    sendingRef.current = false
    setBusy(false)
  }, [])

  const toggleStep = useCallback((stepId: string) => {
    setMsgs((prev) => prev.map((m) => ({
      ...m,
      steps: (m.steps ?? []).map((s) => (s.id === stepId ? { ...s, collapsed: !s.collapsed } : s)),
    })))
  }, [])

  const setMessages = useCallback((next: Msg[]) => {
    setMsgs(next)
    setError(null)
    sidRef.current = null
    sendingRef.current = false
    abortedRef.current = false
  }, [])

  const clearMessages = useCallback(() => {
    setMsgs([])
    setText('')
    setError(null)
    sidRef.current = null
    sendingRef.current = false
    abortedRef.current = false
  }, [setText])

  return { msgs, text, setText, busy, error, send, stop, toggleStep, setMessages, clearMessages }
}

function upsert(prev: Msg[], sid: string | null, mutate: (m: Msg) => Msg): Msg[] {
  if (!sid) return prev
  const i = prev.findIndex((m) => m.id === sid)
  if (i === -1) return prev
  const next = [...prev]
  next[i] = mutate(next[i])
  return next
}

function persistentTool(name: string): boolean {
  return name === 'edit' || name === 'bash' || name === 'write'
}

function terminalStep(s: ToolStep): boolean {
  return s.status === 'done' || s.status === 'error' || s.status === 'blocked'
}

// reImagePath matches image_gen output lines like "  => /abs/path/nekocode_img_xxx.jpg" or "  /path/img.png".
const RE_IMAGE_PATH = /(?:=>\s+)?(\/[^\s]+\.(?:png|jpg|jpeg|gif|webp))/i

function parseImageOutput(output: string): UIImageRef[] {
  if (!output) return []
  const seen = new Set<string>()
  const refs: UIImageRef[] = []
  const matches = output.matchAll(new RegExp(RE_IMAGE_PATH.source, 'gi'))
  for (const m of matches) {
    const p = m[1]
    if (seen.has(p)) continue
    seen.add(p)
    refs.push({ path: p, width: 0, height: 0 })
  }
  return refs
}

function mergeImageRefs(existing: UIImageRef[] | undefined, incoming: UIImageRef[]): UIImageRef[] {
  if (!incoming.length) return existing ?? []
  const seen = new Set((existing ?? []).map((i) => i.path))
  const merged = [...(existing ?? [])]
  for (const ref of incoming) {
    if (!seen.has(ref.path)) {
      seen.add(ref.path)
      merged.push(ref)
    }
  }
  return merged
}
