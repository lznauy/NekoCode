import { EventsOn, Quit } from '../../wailsjs/runtime/runtime'
import {
  Abort,
  ClearSelectedSkill,
  ContextReport,
  ContextSnapshot,
  ContextStatus,
  DeleteSession,
  GetConfig,
  GetSkillManagement,
  ListSessions,
  LoadSession,
  NewSession,
  ProviderModel,
  ReadImageBase64,
  RefreshSkillManagement,
  ReplyConfirm,
  ReplyQuestion,
  SaveConfig,
  SendMessage,
  SetPluginEnabled,
  SelectSkill,
  SwitchModel,
} from '../../wailsjs/go/main/App'
import type { ConfigView } from '../types/config'
import type { ContextSnapshot as GUIContextSnapshot } from '../types/context'
import type { DisplayMessage, SessionMeta } from '../types/session'
import type { SkillManagementView as SkillManagement } from '../types/skills'

export function isWailsEnvironment(): boolean {
  return typeof window !== 'undefined' && (window as unknown as Record<string, unknown>).go !== undefined
}

export function safeEventsOn(event: string, cb: (...args: unknown[]) => void): () => void {
  try {
    return EventsOn(event, cb)
  } catch {
    return () => {}
  }
}

export function safeSendMessage(msg: string): Promise<void> {
  try {
    return SendMessage(msg)
  } catch {
    return Promise.resolve()
  }
}

export function safeAbort(): void {
  try {
    Abort()
  } catch {
    /* noop */
  }
}

export function safeProviderModel(): Promise<string> {
  try {
    return ProviderModel()
  } catch {
    return Promise.resolve('')
  }
}

export function safeSwitchModel(name: string): Promise<string> {
  try {
    return SwitchModel(name)
  } catch {
    return Promise.resolve('')
  }
}

export function safeContextStatus(): Promise<string> {
  try {
    return ContextStatus()
  } catch {
    return Promise.resolve('')
  }
}

export function safeContextReport(): Promise<string> {
  try {
    return ContextReport()
  } catch {
    return Promise.resolve('')
  }
}

export function safeContextSnapshot(): Promise<GUIContextSnapshot | null> {
  try {
    return ContextSnapshot().then((snapshot: unknown) => snapshot as GUIContextSnapshot)
  } catch {
    return Promise.resolve(null)
  }
}

export function safeSelectSkill(name: string): Promise<void> {
  try {
    return SelectSkill(name)
  } catch {
    return Promise.resolve()
  }
}

export function safeClearSelectedSkill(): Promise<void> {
  try {
    return ClearSelectedSkill()
  } catch {
    return Promise.resolve()
  }
}

export function safeGetConfig(): Promise<ConfigView | null> {
  try {
    return GetConfig().then((cfg) => cfg as unknown as ConfigView)
  } catch {
    return Promise.resolve(null)
  }
}

export function safeSaveConfig(cfg: ConfigView): Promise<ConfigView | null> {
  try {
    return SaveConfig(cfg as never).then((saved) => saved as unknown as ConfigView)
  } catch {
    return Promise.resolve(null)
  }
}

export function safeSkillManagementView(): Promise<SkillManagement | null> {
  try {
    return GetSkillManagement().then((view: unknown) => view as SkillManagement)
  } catch {
    return Promise.resolve(null)
  }
}

export function safeRefreshSkillManagement(): Promise<SkillManagement | null> {
  try {
    return RefreshSkillManagement().then((view: unknown) => view as SkillManagement)
  } catch {
    return Promise.resolve(null)
  }
}

export function safeSetPluginEnabled(name: string, enabled: boolean): Promise<SkillManagement | null> {
  try {
    return SetPluginEnabled(name, enabled).then((view: unknown) => view as SkillManagement)
  } catch {
    return Promise.resolve(null)
  }
}

export function safeListSessions(): Promise<SessionMeta[]> {
  try {
    return ListSessions().then((list) => (Array.isArray(list) ? list : []))
  } catch {
    return Promise.resolve([])
  }
}

export function safeNewSession(): Promise<SessionMeta | null> {
  try {
    return NewSession()
  } catch {
    return Promise.resolve(null)
  }
}

export function safeLoadSession(id: string): Promise<DisplayMessage[] | null> {
  try {
    return LoadSession(id).then((msgs) => msgs as unknown as DisplayMessage[])
  } catch {
    return Promise.resolve(null)
  }
}

export function safeDeleteSession(id: string): Promise<void> {
  try {
    return DeleteSession(id)
  } catch {
    return Promise.resolve()
  }
}

export function safeReadImageBase64(path: string): Promise<string | null> {
  try {
    return ReadImageBase64(path)
  } catch {
    return Promise.resolve(null)
  }
}

export function safeReplyConfirm(id: string, ok: boolean): void {
  try {
    ReplyConfirm(id, ok)
  } catch {
    /* noop */
  }
}

export function safeReplyQuestion(id: string, answers: string[][], rejected: boolean): void {
  try {
    ReplyQuestion(id, JSON.stringify(answers), rejected)
  } catch {
    /* noop */
  }
}

export function safeQuit(): void {
  try {
    Quit()
  } catch {
    /* noop */
  }
}
