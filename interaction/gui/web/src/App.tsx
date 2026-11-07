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
  safeInteractionMode,
  safeSetInteractionMode,
	safeCommandMenu,
  safeContextSnapshot,
  safeEventsOn,
  safeGetConfig,
  safeQuit,
  safeSelectSkill,
  safeSkillManagementView,
  safeSwitchModel,
} from './lib/wails'
import type { GUICommandMenu, GUIInteractionMode } from './lib/wails'
import type { ConfirmEvent, Msg, QuestionEvent } from './types/events'
import type { ModelConfig } from './types/config'
import type { protocol } from '../wailsjs/go/models'
type ContextSnapshot = protocol.ContextSnapshot
import type { SkillView } from './types/skills'

export default function App() {
  const { msgs, text, setText, busy, stopping, error: chatError, send, stop, toggleStep, setMessages, clearMessages, imageAttachments, pasteImages } = useChat()
  const [modelRefreshKey, setModelRefreshKey] = useState(0)
  const model = useModelInfo(modelRefreshKey)
  const { containerRef, endRef, follow } = useAutoScroll([msgs])
  const { taRef, resize } = useTextareaResize()

  const {
    sessions,
    currentId,
    loading: sessionsLoading,
    error: sessionError,
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
  const [controlError, setControlError] = useState('')
  const [interactionMode, setInteractionMode] = useState<GUIInteractionMode | null>(null)
  const [modeChanging, setModeChanging] = useState(false)
  const [activeMode, setActiveMode] = useState('')
	const attachmentDraftSessionRef = useRef<string | null>(null)
  const sessionActionRef = useRef(0)
  const [contextOpen, setContextOpen] = useState(false)
  const [contextLoading, setContextLoading] = useState(false)
  const [contextSnapshot, setContextSnapshot] = useState<ContextSnapshot | null>(null)
	const [commandMenu, setCommandMenu] = useState<GUICommandMenu | null>(null)
  const [commandMenuQuery, setCommandMenuQuery] = useState('')
  const activeCommandMenu = !busy && !modeChanging && commandMenuQuery === text.trim() ? commandMenu : null

  useEffect(() => {
    if (modeChanging) return
    let current = true
    void safeInteractionMode().then(state => { if (current) setInteractionMode(state) })
      .catch(err => { if (current) setControlError('读取模式失败：' + String(err)) })
    return () => { current = false }
  }, [currentId, modelRefreshKey, busy, modeChanging])

  useEffect(() => safeEventsOn('mode:changed', () => setModelRefreshKey(key => key + 1)), [])

  useEffect(() => safeEventsOn('agent:input_accepted', (payload: unknown) => {
    const p = payload as {interaction_mode?: {id?: string}; content?: string}
    const id = p.interaction_mode?.id
    if (id) setActiveMode(id + (/^\s*\/plan\s+\S/i.test(p.content ?? '') ? '*' : ''))
  }), [])

  useEffect(() => { if (!busy) setActiveMode('') }, [busy])

  const switchInteractionMode = useCallback(async (id: string) => {
    if (busy || modeChanging) return
    const sessionAction = sessionActionRef.current
    setModeChanging(true)
    setControlError('')
    try {
      const state = await safeSetInteractionMode(id)
      if (sessionAction === sessionActionRef.current) setInteractionMode(state)
    } catch (err) {
      if (sessionAction === sessionActionRef.current) setControlError('切换模式失败：' + String(err))
    } finally { setModeChanging(false) }
  }, [busy, modeChanging])


	useEffect(() => {
	  const input = text.trim()
	  if (busy || (!input.startsWith('/') && !input.startsWith('$'))) {
		setCommandMenu(null)
		return
	  }
      setCommandMenu(null)
	  let current = true
      const publish = (menu: GUICommandMenu | null) => {
        if (!current) return
        setCommandMenuQuery(input)
        setCommandMenu(menu)
      }
	  const load = async () => {
		const direct = await safeCommandMenu(input)
		if (!current) return
		if (direct) {
		  publish(direct)
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
		publish(root)
	  }
      void load().catch((err: unknown) => {
        if (!current) return
        publish(null)
        setControlError('读取命令菜单失败：' + String(err))
      })
	  return () => { current = false }
	}, [busy, text])

  const refreshControls = useCallback(async () => {
    try {
      const [cfg, skillSnapshot] = await Promise.all([safeGetConfig(), safeSkillManagementView()])
      setModels(cfg?.models ?? [])
      setSkills(skillSnapshot?.skills ?? [])
    } catch (err) {
      setControlError('读取控制信息失败：' + String(err))
    }
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
    setControlError('')
    try {
      await safeSwitchModel(name)
      setModelRefreshKey(key => key + 1)
    } catch (err) { setControlError('切换模型失败：' + String(err)) }
  }, [])

  const selectSkill = useCallback(async (name: string) => {
    const sessionAction = sessionActionRef.current
    setControlError('')
    try {
      await safeSelectSkill(name)
      if (sessionAction !== sessionActionRef.current) return
      setSelectedSkill(name)
      void refreshControls()
    } catch (err) { if (sessionAction === sessionActionRef.current) setControlError('选择技能失败：' + String(err)) }
  }, [refreshControls])

  const clearSkill = useCallback(async () => {
    const sessionAction = sessionActionRef.current
    setControlError('')
    try {
      await safeClearSelectedSkill()
      if (sessionAction !== sessionActionRef.current) return
      setSelectedSkill('')
      void refreshControls()
    } catch (err) { if (sessionAction === sessionActionRef.current) setControlError('清除技能失败：' + String(err)) }
  }, [refreshControls])

  const handleTextChange = useCallback(
    (value: string) => {
      setText(value)
      requestAnimationFrame(resize)
    },
    [setText, resize],
  )

  const handleSend = useCallback(() => {
    if (modeChanging) return
    setControlError('')
    send()
    follow()
    requestAnimationFrame(() => {
      if (taRef.current) {
        taRef.current.style.height = 'auto'
      }
    })
  }, [send, taRef, follow, modeChanging])

	const handleCommandSelect = useCallback((item: GUICommandMenu['items'][number]) => {
      if (!activeCommandMenu) return
      setControlError('')
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
	}, [activeCommandMenu, follow, resize, send, setText, taRef])

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
    const action = ++sessionActionRef.current
    const meta = await createSession()
    if (meta && action === sessionActionRef.current) clearMessages()
  }, [createSession, clearMessages])

  const handleSwitchSession = useCallback(
    async (id: string): Promise<Msg[] | null> => {
      if (id === currentId) return null
      if (busy) return null
      const action = ++sessionActionRef.current
      const loaded = await switchSession(id)
      if (loaded && action === sessionActionRef.current) {
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
      const action = ++sessionActionRef.current
      const result = await deleteSession(id)
	  if (result.deleted && wasCurrent && action === sessionActionRef.current) clearMessages()
    },
    [currentId, deleteSession, clearMessages],
  )

  useEffect(() => {
	if (currentId && attachmentDraftSessionRef.current === currentId) {
	  return
	}
	if (currentId && shouldHydrateEmptySession(currentId, attachmentDraftSessionRef.current) && msgs.length === 0 && !sessionsLoading && !busy) {
      const action = sessionActionRef.current
      switchSession(currentId).then((loaded) => {
        if (loaded && action === sessionActionRef.current) {
          setMessages(loaded)
          follow()
        }
      })
    }
  }, [currentId, sessionsLoading, busy, msgs.length, switchSession, setMessages, follow])

  // 监听 agent:confirm 事件
  useEffect(() => {
    const offRequested = safeEventsOn('agent:confirm', (e: unknown) => {
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
    const offResolved = safeEventsOn('agent:confirm_resolved', (e: unknown) => {
      const resolved = e as { id?: string }
      if (resolved?.id) setConfirmEntry((current) => current?.id === resolved.id ? null : current)
    })
    return () => { offRequested(); offResolved() }
  }, [])

  useEffect(() => {
    const offRequested = safeEventsOn('agent:question', (e: unknown) => {
      const qe = e as QuestionEvent
      if (qe?.id && Array.isArray(qe.questions)) {
        setQuestionEntry({
          id: qe.id,
          questions: qe.questions,
        })
      }
    })
    const offResolved = safeEventsOn('agent:question_resolved', (event: unknown) => {
      const resolved = event as { id?: string }
      if (resolved?.id) setQuestionEntry(current => current?.id === resolved.id ? null : current)
    })
    return () => { offRequested(); offResolved() }
  }, [])

  useEffect(() => {
    return safeEventsOn('agent:done', () => {
      setActiveMode('')
      void refreshSessions()
      setModelRefreshKey(key => key + 1)
    })
  }, [refreshSessions])

  useEffect(() => {
	return safeEventsOn('session:changed', (event: unknown) => {
	  const change = event as Parameters<typeof shouldReloadSessionMessages>[0]
      sessionActionRef.current++
	  if (change.attachmentDraft || change.reason === 'new') attachmentDraftSessionRef.current = change.id ?? null
	  else attachmentDraftSessionRef.current = null
	  if (!shouldReloadSessionMessages(change)) return
      setSelectedSkill('')
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
      <div className="grid h-full min-w-0 flex-1 grid-cols-[minmax(0,1fr)] grid-rows-[52px_minmax(0,1fr)_auto] bg-surface-2">
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
        <div className="min-w-0">
          {(controlError || chatError || sessionError) && <div role="alert" className="max-h-24 overflow-y-auto break-words border-t border-danger/30 bg-danger/10 px-5 py-2 text-xs text-danger">{controlError || chatError || sessionError}</div>}
        <InputBar
          text={text}
          busy={busy}
          stopping={stopping}
          interactionMode={interactionMode}
          activeMode={busy ? activeMode : ''}
          modeChanging={modeChanging}
          onSelectMode={switchInteractionMode}
          skills={skills}
          selectedSkill={selectedSkill}
          textareaRef={taRef}
          onChange={handleTextChange}
          onSend={handleSend}
          onStop={stop}
          onTextareaChange={resize}
          onSelectSkill={selectSkill}
          onClearSkill={clearSkill}
		  commandMenu={activeCommandMenu}
		  onSelectCommand={handleCommandSelect}
		  imageAttachments={imageAttachments}
		  onPasteImages={pasteImages}
        />
        </div>
      </div>

      {confirmEntry && (
        <ConfirmDialog
          entry={confirmEntry}
          onDone={() => setConfirmEntry(current => current?.id === confirmEntry.id ? null : current)}
        />
      )}
      {questionEntry && (
        <QuestionDialog
          entry={questionEntry}
          onDone={() => setQuestionEntry(current => current?.id === questionEntry.id ? null : current)}
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
        busy={busy}
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
