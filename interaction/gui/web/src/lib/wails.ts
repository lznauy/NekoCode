import { EventsOn, Quit } from '../../wailsjs/runtime/runtime'
import {
  Abort,
  MCPAuthorizationAction,
  ClearSelectedSkill,
  CommandMenu,
  ContextSnapshot,
  DeleteSession,
  GetConfig,
  GetSkillManagement,
  ListSessions,
  LoadSession,
  NewSession,
  CurrentModel,
  ReadImageBase64,
  RefreshSkillManagement,
  ResolveModelProfile,
  ReplyConfirm,
  ReplyConfirmDecision,
  ReplyQuestion,
  SaveConfig,
  SendMessage,
  SetPluginEnabled,
  SelectSkill,
  SwitchModel,
} from '../../wailsjs/go/main/App'
import type { ConfigView, ModelConfig, ModelProfile } from '../types/config'
import type { protocol, runtime } from '../../wailsjs/go/models'
export type GUICommandMenu = Omit<protocol.CommandMenu, 'convertValues'>
type GUIContextSnapshot = runtime.ContextSnapshot
type SessionMeta = runtime.SessionMeta
type DisplayMessage = runtime.DisplayMessage
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

export function safeCommandMenu(input: string): Promise<GUICommandMenu | null> {
  try {
    return CommandMenu(input).then((menu: unknown) => menu as GUICommandMenu | null)
  } catch {
    return Promise.resolve(null)
  }
}

export function safeAbort(): void {
  try {
    Abort()
  } catch {
    /* noop */
  }
}

export function safeCurrentModel(): Promise<string> {
  try {
    return CurrentModel()
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

export function safeResolveModelProfile(model: ModelConfig): Promise<ModelProfile | null> {
  try {
    const spec = {
      provider: model.provider,
      model: model.model,
      protocol: model.protocol,
      context_window: model.context_window,
    }
    return ResolveModelProfile(spec as never).then((profile) => profile as unknown as ModelProfile)
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

export function safeReplyConfirm(id: string, ok: boolean, remember = false): void {
  try {
    if (remember) {
      ReplyConfirmDecision(id, ok, remember)
      return
    }
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

export function safeMCPAuthorizationAction(name: string, action: string): Promise<void> {
  if (!isWailsEnvironment()) return Promise.reject(new Error('请在 NekoCode 桌面应用中授权'))
  return MCPAuthorizationAction(name, action)
}
