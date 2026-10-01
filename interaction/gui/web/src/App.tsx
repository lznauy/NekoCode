import { useCallback, useEffect, useRef, useState } from 'react'
import { SessionSidebar } from './components/session'
import { TopBar } from './components/TopBar'
import { MessageList } from './components/MessageList'
import { EmptyState } from './components/EmptyState'
import { InputBar } from './components/InputBar'
import { ContextPanel } from './components/ContextPanel'
import ConfirmDialog from './components/ConfirmDialog'
import type { ConfirmEntry } from './components/ConfirmDialog'
import QuestionDialog from './components/QuestionDialog'
import type { QuestionEntry } from './components/QuestionDialog'
import { ConfigPanel } from './components/ConfigPanel'
import type { ConfigTab } from './components/ConfigPanel'
import { SkillPanel } from './components/SkillPanel'
import { useChat } from './hooks/useChat'
import { useModelInfo } from './hooks/useModelInfo'
import { useAutoScroll } from './hooks/useAutoScroll'
import { useTextareaResize } from './hooks/useTextareaResize'
import { mapDisplayMessage, shouldHydrateEmptySession, shouldReloadSessionMessages, useSessions } from './hooks/useSessions'
import { useTheme } from './hooks/useTheme'
import {
  safeClearSelectedSkill,
	safeCommandMenu,
  safeContextSnapshot,
  safeEventsOn,
  safeGetConfig,
  safeQuit,
  safeSelectSkill,
  safeSkillManagementView,
  safeSwitchModel,
} from './lib/wails'
import type { GUICommandMenu } from './lib/wails'
import type { ConfirmEvent, Msg, QuestionEvent } from './types/events'
import type { ModelConfig } from './types/config'
import type { runtime } from '../wailsjs/go/models'
type ContextSnapshot = runtime.ContextSnapshot
import type { SkillView } from './types/skills'

export default function App() {
  const { msgs, text, setText, busy, send, stop, toggleStep, setMessages, clearMessages, imageAttachments, pasteImages } = useChat()
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
  const [configInitialTab, setConfigInitialTab] = useState<ConfigTab>('overview')
  const [skillsOpen, setSkillsOpen] = useState(false)
  const [models, setModels] = useState<ModelConfig[]>([])
  const [skills, setSkills] = useState<SkillView[]>([])
  const [selectedSkill, setSelectedSkill] = useState('')
	const attachmentDraftSessionRef = useRef<string | null>(null)
  const [contextOpen, setContextOpen] = useState(false)
  const [contextLoading, setContextLoading] = useState(false)
  const [contextSnapshot, setContextSnapshot] = useState<ContextSnapshot | null>(null)
	const [commandMenu, setCommandMenu] = useState<GUICommandMenu | null>(null)

	useEffect(() => {
	  const input = text.trim()
	  if (busy || (input !== '/' && input !== '$' && !input.startsWith('/') && !input.startsWith('$'))) {
		setCommandMenu(null)
		return
	  }
	  let current = true
	  const load = async () => {
		const direct = await safeCommandMenu(input)
		if (!current) return
		if (direct) {
		  setCommandMenu(direct)
		  return
		}
		if (/\s/.test(input)) {
		  setCommandMenu(null)
		  return
		}
		const root = await safeCommandMenu(input.startsWith('$') ? '$' : '/')
		if (!current) return
		if (!root) {
		  setCommandMenu(null)
		  return
		}
		root.items = root.items.filter((item) => item.value.startsWith(input))
		setCommandMenu(root)
	  }
	  void load()
	  return () => { current = false }
	}, [busy, text])

  const refreshControls = useCallback(async () => {
    const [cfg, skillSnapshot] = await Promise.all([
      safeGetConfig(),
      safeSkillManagementView(),
    ])
    setModels(cfg?.models ?? [])
    setSkills(skillSnapshot?.skills ?? [])
  }, [])

  useEffect(() => {
    refreshControls()
  }, [refreshControls])

  const openContext = useCallback(async () => {
    setContextOpen(true)
    setContextLoading(true)
    try {
      setContextSnapshot(await safeContextSnapshot())
    } finally {
      setContextLoading(false)
    }
  }, [])

  const switchModel = useCallback(async (name: string) => {
    await safeSwitchModel(name)
    setModelRefreshKey((key) => key + 1)
  }, [])

  const selectSkill = useCallback(async (name: string) => {
    await safeSelectSkill(name)
    setSelectedSkill(name)
    refreshControls()
  }, [refreshControls])

  const clearSkill = useCallback(async () => {
    await safeClearSelectedSkill()
    setSelectedSkill('')
    refreshControls()
  }, [refreshControls])

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

	const handleCommandSelect = useCallback((item: GUICommandMenu['items'][number]) => {
	  if (item.submit) {
		send(item.value)
		follow()
		return
	  }
	  setText(item.value)
	  requestAnimationFrame(() => {
		taRef.current?.focus()
		resize()
	  })
	}, [follow, resize, send, setText, taRef])

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
      if (loaded) {
        setMessages(loaded)
        follow()
      }
      return loaded ?? null
    },
    [busy, currentId, switchSession, setMessages, follow],
  )

  const handleDeleteSession = useCallback(
    async (id: string) => {
      const wasCurrent = id === currentId
      const result = await deleteSession(id)
	  if (result.deleted && wasCurrent) clearMessages()
    },
    [currentId, deleteSession, clearMessages],
  )

  useEffect(() => {
	if (currentId && attachmentDraftSessionRef.current === currentId) {
	  return
	}
	if (currentId && shouldHydrateEmptySession(currentId, attachmentDraftSessionRef.current) && msgs.length === 0 && !sessionsLoading && !busy) {
      switchSession(currentId).then((loaded) => {
        if (loaded) {
          setMessages(loaded)
          follow()
        }
      })
    }
  }, [currentId, sessionsLoading, busy, msgs.length, switchSession, setMessages, follow])

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
          kind: ce.kind ?? 'permission',
          approval: ce.approval,
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

  useEffect(() => {
	return safeEventsOn('session:changed', (event: unknown) => {
	  const change = event as Parameters<typeof shouldReloadSessionMessages>[0]
	  if (change.attachmentDraft) attachmentDraftSessionRef.current = change.id ?? null
	  else attachmentDraftSessionRef.current = null
	  if (!shouldReloadSessionMessages(change)) return
      const messages = change.messages
      setMessages(Array.isArray(messages) ? messages.map(mapDisplayMessage) : [])
      follow()
    })
  }, [setMessages, follow])

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
          models={models}
          busy={busy}
          theme={theme}
          onToggleTheme={toggleTheme}
          onSwitchModel={switchModel}
          onOpenContext={openContext}
          onOpenConfig={() => {
            setConfigInitialTab('overview')
            setConfigOpen(true)
          }}
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
          skills={skills}
          selectedSkill={selectedSkill}
          textareaRef={taRef}
          onChange={handleTextChange}
          onSend={handleSend}
          onStop={stop}
          onTextareaChange={resize}
          onSelectSkill={selectSkill}
          onClearSkill={clearSkill}
		  commandMenu={commandMenu}
		  onSelectCommand={handleCommandSelect}
		  imageAttachments={imageAttachments}
		  onPasteImages={pasteImages}
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
        initialTab={configInitialTab}
        onClose={() => setConfigOpen(false)}
        onSaved={() => {
          setModelRefreshKey((key) => key + 1)
          refreshControls()
        }}
      />
      <ContextPanel
        open={contextOpen}
        snapshot={contextSnapshot}
        loading={contextLoading}
        onClose={() => setContextOpen(false)}
      />
      <SkillPanel
        open={skillsOpen}
        onClose={() => setSkillsOpen(false)}
        onConfigureMcp={() => {
          setSkillsOpen(false)
          setConfigInitialTab('mcp')
          setConfigOpen(true)
        }}
      />
    </div>
  )
}
