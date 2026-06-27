import { EventsOn, Quit } from '../../wailsjs/runtime/runtime'
import {
  Abort,
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
} from '../../wailsjs/go/main/App'
import type { ConfigSnapshot } from '../types/config'
import type { DisplayMessage, SessionMeta } from '../types/session'
import type { SkillManagementSnapshot as SkillManagement } from '../types/skills'

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

export function safeGetConfig(): Promise<ConfigSnapshot | null> {
  try {
    return GetConfig().then((cfg) => cfg as unknown as ConfigSnapshot)
  } catch {
    return Promise.resolve(null)
  }
}

export function safeSaveConfig(cfg: ConfigSnapshot): Promise<ConfigSnapshot | null> {
  try {
    return SaveConfig(cfg as never).then((saved) => saved as unknown as ConfigSnapshot)
  } catch {
    return Promise.resolve(null)
  }
}

export function safeSkillManagementSnapshot(): Promise<SkillManagement | null> {
  try {
    return GetSkillManagement().then((snapshot: unknown) => snapshot as SkillManagement)
  } catch {
    return Promise.resolve(null)
  }
}

export function safeRefreshSkillManagement(): Promise<SkillManagement | null> {
  try {
    return RefreshSkillManagement().then((snapshot: unknown) => snapshot as SkillManagement)
  } catch {
    return Promise.resolve(null)
  }
}

export function safeSetPluginEnabled(name: string, enabled: boolean): Promise<SkillManagement | null> {
  try {
    return SetPluginEnabled(name, enabled).then((snapshot: unknown) => snapshot as SkillManagement)
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
