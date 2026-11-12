import { BackgroundUsage } from "../../wailsjs/go/main/App"
import { ListTasks, TaskHistory, TaskAttempts, TaskOutput, DeleteTask, SearchTaskOutputs, RetryTaskReview, ExportTaskOutputs } from "../../wailsjs/go/main/App"
import { EventsOn, Quit } from '../../wailsjs/runtime/runtime'
import {
  Abort,
  BackgroundDemands, ResumeBackgroundDemand, DiscardBackgroundDemand,
  BackgroundTasks, AppendBackgroundTask, CancelBackgroundTask, StopBackgroundTasks, SubmitBackgroundDemand,
  MCPAuthorizationAction,
  ClearSelectedSkill,
  CommandMenu,
  ContextSnapshot,
  DeleteSession,
  DeleteClipboardImage,
  GetConfig,
  GetSkillManagement,
  ListSessions,
  LoadSession,
  NewSession,
  CurrentModel,
  InteractionMode,
  SetInteractionMode,
  ReadImageBase64,
  RefreshSkillManagement,
  ResolveModelProfile,
  ReplyConfirm,
  ReplyConfirmDecision,
  ReplyQuestion,
  SaveConfig,
  SaveClipboardImage,
  SendMessage,
  SetPluginEnabled,
  SelectSkill,
  SwitchModel,
} from '../../wailsjs/go/main/App'
import type { ConfigView, ModelConfig, ModelProfile } from '../types/config'
import type { core, protocol, runtime } from '../../wailsjs/go/models'
export type GUICommandMenu = Omit<protocol.CommandMenu, 'convertValues'>
type GUIContextSnapshot = protocol.ContextSnapshot
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

export function safeSendMessage(msg: string, images: core.ImageAttachment[] = []): Promise<void> {
	if (!isWailsEnvironment()) return Promise.resolve()
	try {
		return SendMessage(msg, images)
	} catch (err) {
		return Promise.reject(err)
	}
}

export function safeSaveClipboardImage(dataURL: string): Promise<string> {
  try {
    return SaveClipboardImage(dataURL)
  } catch (err) {
    return Promise.reject(err)
  }
}

export function safeDeleteClipboardImage(path: string): Promise<void> {
  try {
    return DeleteClipboardImage(path)
  } catch (err) {
    return Promise.reject(err)
  }
}

export function safeCommandMenu(input: string): Promise<GUICommandMenu | null> {
  try {
    return CommandMenu(input).then((menu: unknown) => menu as GUICommandMenu | null)
  } catch {
    return Promise.resolve(null)
  }
}

export async function safeAbort(): Promise<void> {
  if (!isWailsEnvironment()) return
  await Abort()
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

export async function safeClearSelectedSkill(): Promise<void> {
  await ClearSelectedSkill()
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
    return LoadSession(id)
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

export async function safeReplyConfirm(id: string, ok: boolean, remember = false): Promise<void> {
  if (remember) await ReplyConfirmDecision(id, ok, remember)
  else await ReplyConfirm(id, ok)
}

export async function safeReplyQuestion(id: string, answers: string[][], rejected: boolean): Promise<void> {
  await ReplyQuestion(id, JSON.stringify(answers), rejected)
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

export interface GUIInteractionMode {
  selected: string
  options: Array<{id: string; label: string; description: string}> | null
}

export async function safeInteractionMode(): Promise<GUIInteractionMode | null> {
  if (!isWailsEnvironment()) return null
  // Older hosts can still use /mode through the command entry point.
  const api = (window as unknown as {go?: {main?: {App?: Record<string, unknown>}}}).go?.main?.App
  if (typeof api?.InteractionMode !== 'function') return null
  return InteractionMode()
}

export async function safeSetInteractionMode(id: string): Promise<GUIInteractionMode> {
  if (!isWailsEnvironment()) throw new Error('Interaction modes unavailable')
  return SetInteractionMode(id)
}

export type BackgroundTask = runtime.BackgroundTask
export async function safeBackgroundTasks(): Promise<BackgroundTask[]> {
  if (!isWailsEnvironment()) return []
  return (await BackgroundTasks()) ?? []
}
export async function safeAppendBackgroundTask(sessionID: string, id: string, text: string): Promise<void> {
  if (!isWailsEnvironment()) throw new Error('后台任务不可用')
  return AppendBackgroundTask(sessionID, id, text)
}
export async function safeCancelBackgroundTask(sessionID: string, id: string): Promise<void> {
  if (!isWailsEnvironment()) throw new Error('后台任务不可用')
  return CancelBackgroundTask(sessionID, id)
}
export async function safeStopBackgroundTasks(sessionID: string): Promise<void> {
  if (!isWailsEnvironment()) throw new Error('后台任务不可用')
  return StopBackgroundTasks(sessionID)
}
export async function safeSubmitBackgroundDemand(sessionID: string, text: string): Promise<void> {
  if (!isWailsEnvironment()) throw new Error('后台任务不可用')
  return SubmitBackgroundDemand(sessionID, text)
}

export type BackgroundDemand = runtime.BackgroundDemand
export async function safeBackgroundDemands(sessionID: string): Promise<BackgroundDemand[]> {
  if (!isWailsEnvironment()) return []
  return (await BackgroundDemands(sessionID)) ?? []
}
export async function safeResumeBackgroundDemand(sessionID: string, id: string): Promise<void> {
  if (!isWailsEnvironment()) throw new Error('需求收件箱不可用')
  return ResumeBackgroundDemand(sessionID, id)
}
export async function safeDiscardBackgroundDemand(sessionID: string, id: string): Promise<void> {
  if (!isWailsEnvironment()) throw new Error('需求收件箱不可用')
  return DiscardBackgroundDemand(sessionID, id)
}

export type TaskSummary = runtime.SubagentSummary
export type TaskPage = runtime.SubagentPage
export type TaskSearchPage = runtime.TaskSearchPage
export type TaskHistoryPage = runtime.SubagentHistoryPage
export type TaskAttemptsPage = runtime.SubagentAttemptPage
export type TaskOutputPage = runtime.SubagentOutputPage
export async function safeListTasks(sessionID: string, all: boolean, offset=0): Promise<TaskPage> { return await ListTasks(sessionID, all, offset, 50) }
export async function safeTaskHistory(sessionID: string,id: string,before=0): Promise<TaskHistoryPage> { return await TaskHistory(sessionID,id,before,50) }
export async function safeTaskAttempts(sessionID: string,id: string,before=0): Promise<TaskAttemptsPage> { return await TaskAttempts(sessionID,id,before,50) }
export async function safeTaskOutput(sessionID: string,id: string,sequence: number,offset=0): Promise<TaskOutputPage> { return await TaskOutput(sessionID,id,sequence,offset) }
export async function safeSearchTaskOutputs(sessionID: string,id: string,query: string,after=0): Promise<TaskSearchPage> { return await SearchTaskOutputs(sessionID,id,query,after,20) }
export async function safeDeleteTask(sessionID: string,id: string): Promise<void> { return DeleteTask(sessionID,id) }
export async function safeRetryTaskReview(sessionID: string,id: string): Promise<void> { return RetryTaskReview(sessionID,id) }
export async function safeExportTaskOutputs(sessionID: string,id: string): Promise<string> { return ExportTaskOutputs(sessionID,id) }

export type BackgroundUsageTotal = runtime.BackgroundUsage
export async function safeBackgroundUsage(sessionID: string): Promise<BackgroundUsageTotal> {
 if (!isWailsEnvironment()) return {total_tokens:0,cache_hit_tokens:0,cache_miss_tokens:0,tool_use_count:0,duration_ms:0}
 return BackgroundUsage(sessionID)
}
