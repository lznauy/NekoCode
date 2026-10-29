export interface ModelDefinition {
  name?: string
  model: string
  protocol?: 'openai' | 'anthropic' | ''
  reasoning_effort?: ModelConfig['reasoning_effort']
  context_window?: number
  vision?: boolean
}

export interface ProviderConfig {
  name: string
  base_url?: string
  api_key?: string
  protocol?: 'openai' | 'anthropic' | ''
  models: Array<string | ModelDefinition>
}

export interface ModelConfig {
  name: string
  provider: string
  api_key: string
  model: string
  base_url?: string
  protocol?: 'openai' | 'anthropic' | ''
  reasoning_effort?: '' | 'none' | 'minimal' | 'low' | 'medium' | 'high' | 'xhigh' | 'max'
  context_window?: number
  vision?: boolean
  profile?: ModelProfile
}

export interface ModelProfile {
  context_window: number
  context_window_source: 'override' | 'model' | 'default'
  reasoning_efforts?: string[]
}

export interface ImageGenConfig {
  name: string
  provider: string
  api_key: string
  secret_key: string
  base_url?: string
  model?: string
}

export interface ImageUnderstandConfig {
  name: string
  provider: string
  api_key: string
  model: string
  base_url?: string
  protocol?: 'openai' | 'anthropic' | ''
}

export interface MCPServerConfig {
  url?: string
  oauth_client_id?: string
  oauth_client_secret?: string
  oauth_client_metadata_url?: string
  oauth_callback_port?: number
  command: string
  args?: string[]
  env?: Record<string, string>
  enabled: boolean
}

export interface SandboxConfig {
  sandbox_mode?: string
  network?: boolean
  writable_roots?: string[]
}

export interface PermissionsConfig {
  allow?: string[]
  ask?: string[]
  deny?: string[]
  sandbox?: Record<string, SandboxConfig>
}

export interface WorkspaceConfig {
  path: string
  access?: 'read-only' | 'read-write' | ''
}

export interface JevConfig {
  api_key?: string
  model?: string
  base_url?: string
  keep_threshold?: number | null
  enabled?: boolean | null
}

export interface ConfigView {
  jev?: JevConfig
  path: string
  exists: boolean
  active: string
  auto_compact_percent: number
  max_concurrent_subagents?: number
  compaction_model?: string
  providers: ProviderConfig[]
  models: ModelConfig[]
  image_gen_models?: ImageGenConfig[]
  image_understand_models?: ImageUnderstandConfig[]
  mcp_servers?: Record<string, MCPServerConfig>
  permissions?: PermissionsConfig
  workspaces?: WorkspaceConfig[]
}
