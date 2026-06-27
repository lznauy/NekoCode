export interface SkillSnapshot {
  name: string
  description?: string
  dir?: string
  files?: string[]
  loaded: boolean
  source: string
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
}

export interface SkillManagementSnapshot {
  skills: SkillSnapshot[]
  plugins: PluginSnapshot[]
}
