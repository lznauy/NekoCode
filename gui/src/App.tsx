import { useCallback, useEffect, useRef, useState } from 'react'
import { SessionSidebar } from './components/session'
import { TopBar } from './components/TopBar'
import { MessageList } from './components/MessageList'
import { InputBar } from './components/InputBar'
import ConfirmDialog from './components/ConfirmDialog'
import type { ConfirmEntry } from './components/ConfirmDialog'
import { useChat } from './hooks/useChat'
import { useModelInfo } from './hooks/useModelInfo'
import { useAutoScroll } from './hooks/useAutoScroll'
import { useTextareaResize } from './hooks/useTextareaResize'
import { useSessions } from './hooks/useSessions'
import { useTheme } from './hooks/useTheme'
import { safeEventsOn, safeQuit } from './lib/wails'
import type { ConfirmEvent, Msg } from './types/events'

export default function App() {
  const { msgs, text, setText, busy, send, stop, setMessages, clearMessages } = useChat()
  const model = useModelInfo()
  const { containerRef, endRef } = useAutoScroll([msgs])
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

  const handleTextChange = useCallback(
    (value: string) => {
      setText(value)
      requestAnimationFrame(resize)
    },
    [setText, resize],
  )

  const handleSend = useCallback(() => {
    send()
    requestAnimationFrame(() => {
      if (taRef.current) {
        taRef.current.style.height = 'auto'
      }
    })
  }, [send, taRef])

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
      await deleteSession(id)
      if (wasCurrent) clearMessages()
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
          level: ce.level ?? 0,
        })
      }
    })
  }, [])

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
        <TopBar model={model} busy={busy} theme={theme} onToggleTheme={toggleTheme} onClose={safeQuit} />
        <MessageList ref={containerRef} msgs={msgs} endRef={endRef} />
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
    </div>
  )
}
