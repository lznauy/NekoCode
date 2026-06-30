export interface ModelConfig {
  name: string
  provider: string
  api_key: string
  model: string
  base_url?: string
  protocol?: 'openai' | 'anthropic' | ''
}

export interface ImageGenConfig {
  name: string
  provider: string
  api_key: string
  secret_key: string
  base_url?: string
  model?: string
}

export interface MCPServerConfig {
  command: string
  args?: string[]
  env?: Record<string, string>
  dangerLevel?: 'safe' | 'write' | 'danger' | 'forbidden' | ''
  enabled: boolean
}

export interface ConfigView {
  path: string
  exists: boolean
  active: string
  context_window: number
  flash_model?: string
  models: ModelConfig[]
  image_gen_models?: ImageGenConfig[]
  mcp_servers?: Record<string, MCPServerConfig>
}
