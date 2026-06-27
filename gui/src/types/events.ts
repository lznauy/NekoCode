// Msg types — assistant 消息承载结构化 Run 数据，user/tool 沿用纯文本。
export type Role = 'user' | 'assistant' | 'tool'

export interface ToolStep {
  id: string
  toolName: string
  args: string
  preview?: string
  output?: string
  status: 'pending' | 'running' | 'done' | 'error' | 'blocked'
  isError: boolean
  collapsed: boolean
}

export interface SubAgent {
  id: string
  subType: string
  colorIdx: number
}

export interface TodoItem {
  content: string
  status: 'pending' | 'in_progress' | 'completed'
}

// 运行时图片引用：从 session 加载后，转为小写字段方便组件使用。
export interface UIImageRef {
  path: string
  url?: string
  width: number
  height: number
}

export interface Msg {
  id: string
  role: Role
  text: string
  streamText?: string
  streaming: boolean
  // —— Run 形态 (仅 assistant 流式期填充) ——
  phase?: AgentPhase
  tokens?: { prompt: number; completion: number }
  steps?: ToolStep[]
  reasoning?: string
  reasoningDone?: boolean
  todos?: TodoItem[]
  subagents?: SubAgent[]
  activity?: { reads: number; searches: number; fetches: number; other: number }
  elapsedMs?: number
  compactCount?: number
  // —— 历史会话中携带的图片 ——
  images?: UIImageRef[]
}

// —— 事件载荷类型 ——

export interface DeltaEvent {
  id: number
  delta: string
  done: boolean
}

export interface ReasoningEvent {
  delta: string
  done: boolean
}

export interface PhaseEvent {
  phase: AgentPhase
}

export interface ToolStartPayload {
  id: string
  toolName: string
  args: string
  preview: string
  blocked?: boolean
  reason?: string
}

export interface ToolPreviewPayload {
  toolName: string
  preview: string
}

export interface ToolDonePayload {
  id: string
  toolName: string
  args: string
  output: string
  isError: boolean
}

export interface SubAgentStartPayload {
  id: string
  subType: string
  colorIdx: number
}

export interface SubAgentEndPayload {
  id: string
}

export interface TodosPayload {
  items: TodoItem[]
}

export interface MetricsPayload {
  prompt: number
  completion: number
  cacheHit: number
  cacheMiss: number
  elapsedMs: number
  compactCount: number
}

export interface DoneEvent {
  output: string
  error: string
}

export type AgentPhase =
  | 'ready'
  | 'waiting'
  | 'thinking'
  | 'reasoning'
  | 'running'

export type AgentStatus = 'idle' | 'thinking' | 'running'

export interface StatusEvent {
  status: AgentStatus
}

export interface ConfirmEvent {
  id: string
  toolName: string
  args: Record<string, unknown>
  preview?: string
  level: number
}

export interface QuestionOption {
  label: string
  description?: string
}

export interface QuestionItem {
  header?: string
  question: string
  options?: QuestionOption[]
  multiple?: boolean
  custom?: boolean
}

export interface QuestionEvent {
  id: string
  questions: QuestionItem[]
}

// 向后兼容: 旧的 agent:step 事件仍保留以兜底未分发的 action。
export interface StepEvent {
  action: string
  toolName: string
  toolArgs: string
  output: string
}
