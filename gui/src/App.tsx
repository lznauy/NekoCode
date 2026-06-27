import { useCallback, useEffect, useRef, useState } from 'react'
import { SessionSidebar } from './components/session'
import { TopBar } from './components/TopBar'
import { MessageList } from './components/MessageList'
import { EmptyState } from './components/EmptyState'
import { InputBar } from './components/InputBar'
import ConfirmDialog from './components/ConfirmDialog'
import type { ConfirmEntry } from './components/ConfirmDialog'
import QuestionDialog from './components/QuestionDialog'
import type { QuestionEntry } from './components/QuestionDialog'
import { ConfigPanel } from './components/ConfigPanel'
import { SkillPanel } from './components/SkillPanel'
import { useChat } from './hooks/useChat'
import { useModelInfo } from './hooks/useModelInfo'
import { useAutoScroll } from './hooks/useAutoScroll'
import { useTextareaResize } from './hooks/useTextareaResize'
import { useSessions } from './hooks/useSessions'
import { useTheme } from './hooks/useTheme'
import { safeEventsOn, safeQuit } from './lib/wails'
import type { ConfirmEvent, Msg, QuestionEvent } from './types/events'

export default function App() {
  const { msgs, text, setText, busy, send, stop, toggleStep, setMessages, clearMessages } = useChat()
  const [modelRefreshKey, setModelRefreshKey] = useState(0)
  const model = useModelInfo(modelRefreshKey)
  const { containerRef, endRef, follow } = useAutoScroll([msgs])
  const { taRef, resize } = useTextareaResize()

  const {
    sessions,
    currentId,
    loading: sessionsLoading,
    createSession,
    switchSession,
    deleteSession,
    refresh: refreshSessions,
  } = useSessions()

  const { theme, toggle: toggleTheme } = useTheme()

  // 确认弹窗
  const [confirmEntry, setConfirmEntry] = useState<ConfirmEntry | null>(null)
  const [questionEntry, setQuestionEntry] = useState<QuestionEntry | null>(null)
  const [configOpen, setConfigOpen] = useState(false)
  const [skillsOpen, setSkillsOpen] = useState(false)

  const handleTextChange = useCallback(
    (value: string) => {
      setText(value)
      requestAnimationFrame(resize)
    },
    [setText, resize],
  )

  const handleSend = useCallback(() => {
    send()
    follow()
    requestAnimationFrame(() => {
      if (taRef.current) {
        taRef.current.style.height = 'auto'
      }
    })
  }, [send, taRef, follow])

  const handlePromptSelect = useCallback(
    (prompt: string) => {
      setText(prompt)
      requestAnimationFrame(() => {
        taRef.current?.focus()
        resize()
      })
    },
    [resize, setText, taRef],
  )

  const handleCreateSession = useCallback(async () => {
    const meta = await createSession()
    if (meta) clearMessages()
  }, [createSession, clearMessages])

  const handleSwitchSession = useCallback(
    async (id: string): Promise<Msg[] | null> => {
      if (id === currentId) return null
      if (busy) return null
      const loaded = await switchSession(id)
      if (loaded) setMessages(loaded)
      return loaded ?? null
    },
    [busy, currentId, switchSession, setMessages],
  )

  const handleDeleteSession = useCallback(
    async (id: string) => {
      const wasCurrent = id === currentId
      const remaining = await deleteSession(id)
      if (wasCurrent || remaining.length === 0) clearMessages()
    },
    [currentId, deleteSession, clearMessages],
  )

  useEffect(() => {
    if (currentId && msgs.length === 0 && !sessionsLoading && !busy) {
      switchSession(currentId).then((loaded) => {
        if (loaded) setMessages(loaded)
      })
    }
  }, [currentId, sessionsLoading, busy, msgs.length, switchSession, setMessages])

  // 监听 agent:confirm 事件
  useEffect(() => {
    return safeEventsOn('agent:confirm', (e: unknown) => {
      const ce = e as ConfirmEvent
      if (ce?.id && ce?.toolName) {
        setConfirmEntry({
          id: ce.id,
          toolName: ce.toolName,
          args: ce.args ?? {},
          preview: ce.preview ?? '',
          level: ce.level ?? 0,
        })
      }
    })
  }, [])

  useEffect(() => {
    return safeEventsOn('agent:question', (e: unknown) => {
      const qe = e as QuestionEvent
      if (qe?.id && Array.isArray(qe.questions)) {
        setQuestionEntry({
          id: qe.id,
          questions: qe.questions,
        })
      }
    })
  }, [])

  useEffect(() => {
    return safeEventsOn('agent:done', () => {
      refreshSessions()
    })
  }, [refreshSessions])

  const showEmptyWorkspace = !sessionsLoading && sessions.length === 0 && msgs.length === 0

  return (
    <div className="flex h-full bg-surface text-text">
      <SessionSidebar
        sessions={sessions}
        currentId={currentId}
        loading={sessionsLoading}
        onCreate={handleCreateSession}
        onSwitch={handleSwitchSession}
        onDelete={handleDeleteSession}
      />
      <div className="grid h-full min-w-0 flex-1 grid-rows-[52px_1fr_auto] bg-surface-2">
        <TopBar
          model={model}
          busy={busy}
          theme={theme}
          onToggleTheme={toggleTheme}
          onOpenConfig={() => setConfigOpen(true)}
          onOpenSkills={() => setSkillsOpen(true)}
          onClose={safeQuit}
        />
        {showEmptyWorkspace ? (
          <main className="min-h-0 overflow-y-auto px-5 py-6">
            <EmptyState onPromptSelect={handlePromptSelect} />
          </main>
        ) : (
          <MessageList ref={containerRef} msgs={msgs} endRef={endRef} toggleStep={toggleStep} onPromptSelect={handlePromptSelect} />
        )}
        <InputBar
          text={text}
          busy={busy}
          model={model}
          textareaRef={taRef}
          onChange={handleTextChange}
          onSend={handleSend}
          onStop={stop}
          onTextareaChange={resize}
        />
      </div>

      {confirmEntry && (
        <ConfirmDialog
          entry={confirmEntry}
          onDone={() => setConfirmEntry(null)}
        />
      )}
      {questionEntry && (
        <QuestionDialog
          entry={questionEntry}
          onDone={() => setQuestionEntry(null)}
        />
      )}
      <ConfigPanel
        open={configOpen}
        onClose={() => setConfigOpen(false)}
        onSaved={() => setModelRefreshKey((key) => key + 1)}
      />
      <SkillPanel
        open={skillsOpen}
        onClose={() => setSkillsOpen(false)}
      />
    </div>
  )
}
