import { useCallback, useRef, useState } from 'react'
import type { MutableRefObject } from 'react'
import { genId } from '../lib/id'
import { isUnifiedDiffContent } from '../lib/diffFormat'
import { safeAbort, safeDeleteClipboardImage, safeSaveClipboardImage, safeSendMessage } from '../lib/wails'
import { useWailsEvents } from './useWailsEvents'
import type {
  CompactionEvent,
  AgentPhase,
  MetricsPayload,
  Msg,
  SubAgent,
  TodoItem,
  ToolStep,
  InputAcceptedEvent,
} from '../types/events'
import type { UIImageRef } from '../types/events'

export interface UseChatReturn {
  msgs: Msg[]
  text: string
  setText: (text: string) => void
  busy: boolean
  error: string | null
  send: (input?: string) => void
  stop: () => void
  toggleStep: (stepId: string) => void
  setMessages: (msgs: Msg[]) => void
  clearMessages: () => void
  imageAttachments: ImageInputAttachment[]
  pasteImages: (files: File[], start: number, end: number) => Promise<void>
}

export interface ImageInputAttachment {
  label: string
  path: string
}

interface PendingSubmission {
  draftText: string
  draftImages: ImageInputAttachment[]
  draftGeneration: number
  acceptedImagePaths: Set<string>
}

const MAX_IMAGE_ATTACHMENTS = 8

const emptyRunMsg = (id: string): Msg => ({
  id,
  role: 'assistant',
  text: '',
  streamText: '',
  streaming: true,
  phase: 'thinking' as AgentPhase,
  tokens: { prompt: 0, completion: 0 },
  steps: [],
  reasoning: '',
  reasoningDone: false,
  todos: [],
  subagents: [],
  activity: { reads: 0, searches: 0, fetches: 0, other: 0 },
  elapsedMs: 0,
  compactCount: 0,
})

export function useChat(): UseChatReturn {
  const [msgs, setMsgs] = useState<Msg[]>([])
  const [text, setTextState] = useState('')
  const [imageAttachments, setImageAttachments] = useState<ImageInputAttachment[]>([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const sidRef = useRef<string | null>(null)
  const userSidRef = useRef<string | null>(null)
  const sendingRef = useRef(false)
  const abortedRef = useRef(false)
  const textBufferRef = useRef('')
  const reasoningBufferRef = useRef('')
  const textDoneRef = useRef(false)
  const reasoningDoneRef = useRef(false)
  const activityBufferRef = useRef({ reads: 0, searches: 0, fetches: 0, other: 0 })
  const hasStreamTextRef = useRef(false)
  const streamBreakPendingRef = useRef(false)
  const flushTimerRef = useRef<number | null>(null)
	const nextImageIDRef = useRef(0)
	const draftGenerationRef = useRef(0)
	const pendingSubmissionRef = useRef<PendingSubmission | null>(null)

	const deleteClipboardImage = useCallback((path: string) => {
	  void safeDeleteClipboardImage(path).catch((err: unknown) => {
		const detail = String(err)
		console.error(`Failed to clean up clipboard image ${path}:`, err)
		if (detail.includes('application is shutting down')) return
		setError((current) => current ?? `清理图片附件失败：${detail}`)
	  })
	}, [])

	const settlePendingSubmission = useCallback((restoreCurrentDraft: boolean) => {
	  const pending = pendingSubmissionRef.current
	  pendingSubmissionRef.current = null
	  if (!pending || pending.draftImages.length === 0) return
	  const unaccepted = pending.draftImages.filter((image) => !pending.acceptedImagePaths.has(image.path))
	  if (unaccepted.length === 0) return
	  if (restoreCurrentDraft && pending.draftGeneration === draftGenerationRef.current) {
		const acceptedLabels = pending.draftImages
		  .filter((image) => pending.acceptedImagePaths.has(image.path))
		  .map((image) => image.label)
		const restoredText = acceptedLabels.reduce((value, label) => value.split(label).join(''), pending.draftText).trim()
		setTextState((current) => current === '' ? restoredText : current)
		setImageAttachments((current) => current.length === 0 ? unaccepted : current)
		return
	  }
	  for (const image of unaccepted) deleteClipboardImage(image.path)
	}, [deleteClipboardImage])

  const setText = useCallback((next: string) => {
	draftGenerationRef.current += 1
    setTextState(next)
    setImageAttachments((current) => {
      const kept = current.filter((image) => next.includes(image.label))
      for (const image of current) {
		if (!kept.includes(image)) deleteClipboardImage(image.path)
      }
      return kept
    })
  }, [deleteClipboardImage])

  const pasteImages = useCallback(async (files: File[], start: number, end: number) => {
	const generation = draftGenerationRef.current
	if (imageAttachments.length + files.length > MAX_IMAGE_ATTACHMENTS) {
	  setError(`单条消息最多附加 ${MAX_IMAGE_ATTACHMENTS} 张图片`)
	  return
	}
    const saved: ImageInputAttachment[] = []
    try {
      for (const file of files) {
		const dataURL = await fileDataURL(file)
		const path = await safeSaveClipboardImage(dataURL)
		let label: string
		do {
		  nextImageIDRef.current += 1
		  label = `[Image #${nextImageIDRef.current}]`
		} while (text.includes(label) || saved.some((image) => image.label === label))
		saved.push({ label, path })
      }
    } catch (err) {
	  for (const image of saved) deleteClipboardImage(image.path)
      setError(String(err))
      return
    }
	if (generation !== draftGenerationRef.current) {
	  for (const image of saved) deleteClipboardImage(image.path)
	  return
	}
    if (saved.length === 0) return
	const replaced = imageAttachments.filter((image) => {
	  const markerStart = text.indexOf(image.label)
	  return markerStart >= 0 && start < markerStart + image.label.length && end > markerStart
	})
	for (const image of replaced) deleteClipboardImage(image.path)
    setImageAttachments((current) => [...current.filter((image) => !replaced.includes(image)), ...saved])
    setTextState((current) => insertImageMarkers(current, saved.map((image) => image.label), start, end))
  }, [imageAttachments, text, deleteClipboardImage])

  const flushBuffers = useCallback(() => {
    flushTimerRef.current = null
    const sid = sidRef.current
    if (!sid) return
    const textDelta = textBufferRef.current
    const reasoningDelta = reasoningBufferRef.current
    const textDone = textDoneRef.current
    const reasoningDone = reasoningDoneRef.current
    const activityDelta = activityBufferRef.current
    const hasActivity = activityDelta.reads > 0 || activityDelta.searches > 0 || activityDelta.fetches > 0 || activityDelta.other > 0
    if (!textDelta && !reasoningDelta && !textDone && !reasoningDone && !hasActivity) return

    textBufferRef.current = ''
    reasoningBufferRef.current = ''
    textDoneRef.current = false
    reasoningDoneRef.current = false
    activityBufferRef.current = { reads: 0, searches: 0, fetches: 0, other: 0 }

    setMsgs((prev) => upsert(prev, sid, (m) => ({
      ...m,
      streamText: textDelta ? (m.streamText ?? '') + textDelta : m.streamText,
      streaming: textDone ? false : m.streaming,
      reasoning: reasoningDelta ? (m.reasoning ?? '') + reasoningDelta : m.reasoning,
      reasoningDone: reasoningDone ? true : m.reasoningDone,
      activity: hasActivity ? addActivity(m.activity, activityDelta) : m.activity,
    })))
  }, [])

  const scheduleFlush = useCallback(() => {
    if (flushTimerRef.current !== null) return
    flushTimerRef.current = window.setTimeout(flushBuffers, 33)
  }, [flushBuffers])

  // 顺着 phase 切换更新 UI 状态。
  const onPhase = useCallback((e: { phase: AgentPhase }) => {
    setMsgs((prev) => upsert(prev, sidRef.current, (m) => ({ ...m, phase: e.phase })))
  }, [])

  const onDelta = useCallback((e: { id: number; delta: string; done: boolean }) => {
    if (!sidRef.current) return
    textBufferRef.current += e.delta
    if (e.delta.trim()) {
      hasStreamTextRef.current = true
      streamBreakPendingRef.current = false
    }
    if (e.done) textDoneRef.current = true
    scheduleFlush()
  }, [scheduleFlush])

  const onReasoning = useCallback((e: { delta: string; done: boolean }) => {
    if (!sidRef.current) return
    reasoningBufferRef.current += e.delta
    if (e.done) reasoningDoneRef.current = true
    scheduleFlush()
  }, [scheduleFlush])

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
    if (interactiveTool(e.toolName)) return
    markToolBoundary(textBufferRef, hasStreamTextRef, streamBreakPendingRef)
    if (compactTool(e.toolName) && !e.blocked) {
      activityBufferRef.current = addActivity(activityBufferRef.current, activityForTool(e.toolName))
      scheduleFlush()
      return
    }
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
    if (interactiveTool(e.toolName)) return
    if (compactTool(e.toolName)) return
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
    if (interactiveTool(e.toolName)) return
    setMsgs((prev) => upsert(prev, sid, (m) => {
      const steps = [...(m.steps ?? [])]
      const byId = e.id ? steps.findIndex((s) => s.id === e.id && !terminalStep(s)) : -1
      const idx = byId !== -1 ? byId : steps.findIndex((s) => s.toolName === e.toolName && !terminalStep(s))
      if (idx === -1 && compactTool(e.toolName) && !e.isError) return m
      if (idx === -1 && compactTool(e.toolName) && e.isError) {
        return {
          ...m,
          steps: [...steps, {
            id: e.id || genId(),
            toolName: e.toolName,
            args: e.args,
            output: e.output,
            status: 'error',
            isError: true,
            collapsed: false,
          }],
        }
      }
      if (idx === -1) return m
      const target = steps[idx]
      const isPersistent = persistentTool(e.toolName)
      const isEdit = e.toolName === 'edit'
      let output = e.output
      let preview = target.preview

      if (isEdit) {
        // 与 TUI finishToolBlock 一致：edit 成功时用最终输出替换运行时 preview，
        // 保证 relocated/rebased edits 展示准确提交 diff。
        if (e.isError || !isUnifiedDiffContent(output)) {
          preview = undefined
        } else {
          preview = output
          output = ''
        }
      } else if (!isPersistent && !e.isError) {
        // 非持久化工具在成功后丢弃中间 preview/output，与 session view 的持久工具集合一致。
        output = ''
        preview = undefined
      }

      steps[idx] = {
        ...target,
        output,
        preview,
        isError: e.isError,
        status: e.isError ? 'error' : 'done',
        // edit/shell 默认保持展开，write/diff 默认收起，与 ActivityRow 原本地状态一致。
        collapsed: !(e.toolName === 'edit' || e.toolName === 'shell'),
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

  const onDone = useCallback((e: { output?: string; error: string }) => {
    if (abortedRef.current) return
    flushBuffers()
    // A run error is unrelated to an in-flight compaction: settle with the
    // fixed note so the card never shows a foreign error (the run's own
    // failure is displayed separately below).
    setMsgs((prev) => settleCompactions(prev, '压缩已结束，未收到完成详情'))
    const sid = sidRef.current
    if (e.error) {
      setError(e.error)
      setMsgs((prev) => [
        ...prev,
        { id: genId(), role: 'assistant' as const, text: 'Error: ' + e.error, streaming: false },
      ])
    }
    if (sid) {
      setMsgs((prev) => prev.map((m) => {
        if (m.id !== sid) return m
        const finalText = (e.output ?? '').trim()
        // 防御性过滤：后端已过滤系统消息，此处兜底防止流式残留
        const safeText = isSystemOutput(finalText) ? '' : finalText
        const finalSteps = (m.steps ?? []).filter((s) => persistentTool(s.toolName))
        const keepRunCard = finalSteps.length > 0 || !!m.images?.length
        if (!e.error && !keepRunCard) {
          return {
            ...m,
            text: safeText || (m.text && !isSystemOutput(m.text) ? m.text : m.streamText && !isSystemOutput(m.streamText ?? '') ? m.streamText : ''),
            streamText: '',
            streaming: false,
            phase: undefined,
            tokens: undefined,
            steps: undefined,
            reasoning: undefined,
            reasoningDone: undefined,
            todos: undefined,
            subagents: undefined,
            activity: undefined,
          }
        }
        return {
          ...m,
          text: safeText || (m.text && !isSystemOutput(m.text) ? m.text : m.streamText && !isSystemOutput(m.streamText ?? '') ? m.streamText : ''),
          streamText: '',
          streaming: false,
          phase: 'ready',
          steps: finalSteps,
          reasoning: '',
          reasoningDone: true,
          todos: [],
          subagents: [],
          activity: undefined,
        }
      }))
    }
    sidRef.current = null
    userSidRef.current = null
    sendingRef.current = false
	settlePendingSubmission(true)
  }, [flushBuffers, settlePendingSubmission])

  const onStatus = useCallback((e: { status: string }) => {
    if (abortedRef.current) return
    setBusy(e.status !== 'idle')
  }, [])

  const onInputAccepted = useCallback((e: InputAcceptedEvent) => {
	if (e.source?.kind !== 'gui') return
	const pending = pendingSubmissionRef.current
	if (!pending) return
	for (const image of e.images ?? []) pending.acceptedImagePaths.add(image.path)
  }, [])

  // onSystem 处理命令输出（/devices、/config 等）：作为独立 system 消息展示。
  // 手动 /compact 的统计已由 CompactionDetails 卡片渲染，命令的文本回显
  // 会重复显示同一结果，此处沿用 TUI 的去重守卫跳过。
  const onSystem = useCallback((e: { content: string }) => {
    const content = (e.content ?? '').trim()
    if (!content) return
    setMsgs((prev) => {
      if (hasSettledManualCompaction(prev) && isCompactionEcho(content)) return prev
      return [
        ...prev,
        { id: genId(), role: 'system' as const, text: content, streaming: false },
      ]
    })
  }, [])

  useWailsEvents({
    onCompaction: (e: CompactionEvent) => {
      if (abortedRef.current) return
      const sid = sidRef.current
      setMsgs((prev) => {
        const index = prev.findIndex((m) => m.compaction?.id === e.id)
        const old = index < 0 ? undefined : prev[index].compaction
        if (old && (old.status === 'completed' || old.status === 'failed')) return prev
        const compaction = { ...e, summary: e.status === 'delta' ? (old?.summary ?? '') + (e.delta ?? '') : (e.summary ?? old?.summary ?? '') }
        const msg: Msg = { id: `compaction-${e.id}`, role: 'system', text: '', streaming: false, compaction }
        if (index < 0) {
          const runIndex = prev.findIndex((m) => m.id === sid)
          if (runIndex >= 0) return [...prev.slice(0, runIndex), msg, ...prev.slice(runIndex)]
          return [...prev, msg]
        }
        return prev.map((m, i) => i === index ? msg : m)
      })
    },
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
    onSystem,
	onInputAccepted,
  })

  const send = useCallback((input?: string) => {
	const t = (input ?? text).trim()
    if (!t || busy || sendingRef.current) return
	const sendImages = input === undefined ? imageAttachments : []
	const draftText = text
	const draftImages = imageAttachments
	const draftGeneration = draftGenerationRef.current
	pendingSubmissionRef.current = {
	  draftText,
	  draftImages,
	  draftGeneration,
	  acceptedImagePaths: new Set<string>(),
	}

    resetBuffers(textBufferRef, reasoningBufferRef, textDoneRef, reasoningDoneRef, activityBufferRef, hasStreamTextRef, streamBreakPendingRef, flushTimerRef)
    sendingRef.current = true
    abortedRef.current = false
	setBusy(true)
    setError(null)
    const userSid = genId()
    userSidRef.current = userSid
    setMsgs((prev) => [...prev, { id: userSid, role: 'user' as const, text: t, streaming: false }])
    setTextState('')
    setImageAttachments([])
	nextImageIDRef.current = 0
    const sid = genId()
    sidRef.current = sid
    setMsgs((prev) => [...prev, emptyRunMsg(sid)])

	void safeSendMessage(t, sendImages).catch((err: unknown) => {
      const errStr = String(err)
	  setError(errStr)
	  if (draftGeneration !== draftGenerationRef.current) {
		for (const image of draftImages) deleteClipboardImage(image.path)
		setMsgs((prev) => [
		  ...prev.filter((message) => message.id !== userSid && message.id !== sid),
		  { id: genId(), role: 'assistant' as const, text: 'Error: ' + errStr, streaming: false },
		])
		setBusy(false)
		sidRef.current = null
		userSidRef.current = null
		sendingRef.current = false
		pendingSubmissionRef.current = null
		return
	  }
	  setMsgs((prev) => [
		...prev.filter((message) => message.id !== userSid && message.id !== sid),
		{ id: genId(), role: 'assistant' as const, text: 'Error: ' + errStr, streaming: false },
	  ])
	  setTextState((current) => current === '' ? draftText : current)
	  setImageAttachments((current) => current.length === 0 ? draftImages : current)
	  pendingSubmissionRef.current = null
      setBusy(false)
      sidRef.current = null
      userSidRef.current = null
      sendingRef.current = false
    })
	}, [text, busy, imageAttachments, deleteClipboardImage])

  const stop = useCallback(() => {
	settlePendingSubmission(true)
	draftGenerationRef.current += 1
    abortedRef.current = true
    flushBuffers()
    setMsgs((prev) => settleCompactions(prev, '已取消，未收到压缩完成确认'))
    safeAbort()
    const sid = sidRef.current
    const userSid = userSidRef.current
    if (sid || userSid) {
      setMsgs((prev) => prev.filter((m) => m.id !== sid && m.id !== userSid))
    }
    sidRef.current = null
    userSidRef.current = null
    sendingRef.current = false
    setBusy(false)
  }, [flushBuffers, settlePendingSubmission])

  const toggleStep = useCallback((stepId: string) => {
    setMsgs((prev) => prev.map((m) => ({
      ...m,
      steps: (m.steps ?? []).map((s) => (s.id === stepId ? { ...s, collapsed: !s.collapsed } : s)),
    })))
  }, [])

  const setMessages = useCallback((next: Msg[]) => {
	settlePendingSubmission(false)
	draftGenerationRef.current += 1
    resetBuffers(textBufferRef, reasoningBufferRef, textDoneRef, reasoningDoneRef, activityBufferRef, hasStreamTextRef, streamBreakPendingRef, flushTimerRef)
    setMsgs(next)
    setImageAttachments((current) => {
	  for (const image of current) deleteClipboardImage(image.path)
      return []
    })
    setTextState('')
    nextImageIDRef.current = 0
    setError(null)
    sidRef.current = null
    userSidRef.current = null
    sendingRef.current = false
    abortedRef.current = false
	}, [deleteClipboardImage, settlePendingSubmission])

  const clearMessages = useCallback(() => {
	settlePendingSubmission(false)
	draftGenerationRef.current += 1
    resetBuffers(textBufferRef, reasoningBufferRef, textDoneRef, reasoningDoneRef, activityBufferRef, hasStreamTextRef, streamBreakPendingRef, flushTimerRef)
    setMsgs([])
    setText('')
    nextImageIDRef.current = 0
    setError(null)
    sidRef.current = null
    userSidRef.current = null
    sendingRef.current = false
    abortedRef.current = false
  }, [setText, settlePendingSubmission])

  return { msgs, text, setText, busy, error, send, stop, toggleStep, setMessages, clearMessages, imageAttachments, pasteImages }
}

function fileDataURL(file: File): Promise<string> {
	if (file.size > 20 * 1024 * 1024) return Promise.reject(new Error('剪贴板图片超过 20 MiB 限制'))
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onerror = () => reject(reader.error ?? new Error('读取剪贴板图片失败'))
    reader.onload = () => typeof reader.result === 'string' ? resolve(reader.result) : reject(new Error('剪贴板图片格式无效'))
    reader.readAsDataURL(file)
  })
}

function insertImageMarkers(text: string, markers: string[], start: number, end: number): string {
  const safeStart = Math.max(0, Math.min(start, text.length))
  const safeEnd = Math.max(safeStart, Math.min(end, text.length))
  const before = text.slice(0, safeStart)
  const after = text.slice(safeEnd)
  const prefix = before && !/\s$/.test(before) ? ' ' : ''
  const suffix = after && !/^\s/.test(after) ? ' ' : ''
  return before + prefix + markers.join(' ') + suffix + after
}

function settleCompactions(msgs: Msg[], error: string): Msg[] {
  return msgs.map((m) => m.compaction && (m.compaction.status === 'started' || m.compaction.status === 'delta')
    ? { ...m, compaction: { ...m.compaction, status: 'failed', error } } : m)
}

// A finished manual compaction already rendered its result card; the command
// layer's plain-text echo of the same result would duplicate it.
function hasSettledManualCompaction(msgs: Msg[]): boolean {
  return msgs.some((m) => m.compaction?.trigger === 'manual' && m.compaction.status !== 'started' && m.compaction.status !== 'delta')
}

function isCompactionEcho(content: string): boolean {
  return content.startsWith('Compacted:') || content.startsWith('Summary updated:') || content.startsWith('Compaction failed:')
}

function upsert(prev: Msg[], sid: string | null, mutate: (m: Msg) => Msg): Msg[] {
  if (!sid) return prev
  const i = prev.findIndex((m) => m.id === sid)
  if (i === -1) return prev
  const next = [...prev]
  next[i] = mutate(next[i])
  return next
}

function markToolBoundary(
  textBufferRef: MutableRefObject<string>,
  hasStreamTextRef: MutableRefObject<boolean>,
  streamBreakPendingRef: MutableRefObject<boolean>,
): void {
  if (!hasStreamTextRef.current || streamBreakPendingRef.current) return
  if (textBufferRef.current.endsWith('\n')) {
    streamBreakPendingRef.current = true
    return
  }
  textBufferRef.current = textBufferRef.current.replace(/\s*$/, '') + '\n'
  streamBreakPendingRef.current = true
}

function interactiveTool(name: string): boolean {
  return name === 'todo_write' || name === 'question'
}

function compactTool(name: string): boolean {
  return name === 'read' || name === 'tsread' || name === 'list' || name === 'grep' || name === 'glob' || name === 'searchfiles' || name === 'webfetch' || name === 'fetch'
}

function activityForTool(toolName: string): NonNullable<Msg['activity']> {
  if (toolName === 'read' || toolName === 'tsread') {
    return { reads: 1, searches: 0, fetches: 0, other: 0 }
  }
  if (toolName === 'grep' || toolName === 'glob' || toolName === 'searchfiles' || toolName === 'list') {
    return { reads: 0, searches: 1, fetches: 0, other: 0 }
  }
  if (toolName === 'webfetch' || toolName === 'fetch') {
    return { reads: 0, searches: 0, fetches: 1, other: 0 }
  }
  return { reads: 0, searches: 0, fetches: 0, other: 1 }
}

function addActivity(current: Msg['activity'] | undefined, delta: NonNullable<Msg['activity']>): NonNullable<Msg['activity']> {
  const base = current ?? { reads: 0, searches: 0, fetches: 0, other: 0 }
  return {
    reads: base.reads + delta.reads,
    searches: base.searches + delta.searches,
    fetches: base.fetches + delta.fetches,
    other: base.other + delta.other,
  }
}

function persistentTool(name: string): boolean {
  return name === 'edit' || name === 'diff' || name === 'shell' || name === 'write'
}

function resetBuffers(
  textBufferRef: MutableRefObject<string>,
  reasoningBufferRef: MutableRefObject<string>,
  textDoneRef: MutableRefObject<boolean>,
  reasoningDoneRef: MutableRefObject<boolean>,
  activityBufferRef: MutableRefObject<NonNullable<Msg['activity']>>,
  hasStreamTextRef: MutableRefObject<boolean>,
  streamBreakPendingRef: MutableRefObject<boolean>,
  flushTimerRef: MutableRefObject<number | null>,
): void {
  textBufferRef.current = ''
  reasoningBufferRef.current = ''
  textDoneRef.current = false
  reasoningDoneRef.current = false
  activityBufferRef.current = { reads: 0, searches: 0, fetches: 0, other: 0 }
  hasStreamTextRef.current = false
  streamBreakPendingRef.current = false
  if (flushTimerRef.current !== null) {
    clearTimeout(flushTimerRef.current)
    flushTimerRef.current = null
  }
}

/** isSystemOutput 检测文本是否为系统内部消息，与后端 isSystemMessage 保持一致。 */
function isSystemOutput(text: string): boolean {
  const t = text.trim()
  return t.startsWith('[System]') || t.startsWith('[Agent stopped:')
}

function terminalStep(s: ToolStep): boolean {
  return s.status === 'done' || s.status === 'error' || s.status === 'blocked'
}

// reImagePath matches image_gen output lines like "  => /abs/path/nekocode_img_xxx.jpg" or "  /path/img.png".
const RE_IMAGE_PATH = /^\s*(?:=>\s+)?(\/[^\s]+\.(?:png|jpg|jpeg|gif|webp))\s*$/i

function parseImageOutput(output: string): UIImageRef[] {
  if (!output) return []
  const seen = new Set<string>()
  const refs: UIImageRef[] = []
  for (const line of output.split(/\r?\n/)) {
    const match = line.match(RE_IMAGE_PATH)
    if (!match) continue
    const p = match[1]
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
