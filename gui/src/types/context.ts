export interface ContextSegment {
  key: string
  label: string
  tokens: number
  tone: string
}

export interface ContextSnapshot {
  budget: number
  used: number
  free: number
  percentUsed: number
  systemPrompt: number
  toolDefTokens: number
  todoText: number
  skillList: number
  messageTokens: number
  toolDefCount: number
  messageCount: number
  userMessages: number
  assistantMsgs: number
  toolResults: number
  archived: number
  compactCount: number
  trimCount: number
  cacheHitTokens: number
  cacheMissTokens: number
  cacheHitRatio: number
  subCount: number
  subTokens: number
  subCacheHit: number
  subCacheMiss: number
  segments: ContextSegment[]
}
