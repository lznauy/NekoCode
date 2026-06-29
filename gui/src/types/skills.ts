export type SkillSourceKind = 'builtin' | 'local' | 'plugin'

export interface SkillSnapshot {
  name: string
  description?: string
  dir?: string
  files?: string[]
  loaded: boolean
  source: string
  sourceKind: SkillSourceKind
  plugin?: string
}

export interface PluginSnapshot {
  name: string
  version?: string
  description?: string
  source?: string
  dir?: string
  enabled: boolean
  skills?: string[]
  skillNames?: string[]
  agents?: string[]
  commands?: string[]
  mcpServers?: string[]
  hasHooks?: boolean
}

export interface MCPServerSnapshot {
  name: string
  plugin: string
  command: string
  args?: string[]
  dangerLevel?: string
  pluginEnabled: boolean
  status?: 'ready' | 'error' | 'disabled' | 'starting' | 'unknown' | string
  error?: string
  toolCount?: number
}

export interface SkillManagementSnapshot {
  skills: SkillSnapshot[]
  plugins: PluginSnapshot[]
  mcp: MCPServerSnapshot[]
}
