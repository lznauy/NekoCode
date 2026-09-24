import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ConfigPanel } from '../ConfigPanel'
import type { ConfigView, ModelConfig } from '../../types/config'
import { ResolveModelProfile, SaveConfig, MCPAuthorizationAction } from '../../../wailsjs/go/main/App'

const config: ConfigView = {
  path: '/tmp/config.json',
  exists: true,
  active: 'main',
  auto_compact_percent: 80,
  models: [
    {
      name: 'main',
      provider: 'openai',
      api_key: '',
      model: 'gpt-5',
      base_url: '',
      protocol: 'openai',
      reasoning_effort: 'medium',
      profile: { context_window: 400000, context_window_source: 'model', reasoning_efforts: ['minimal', 'low', 'medium', 'high'] },
    },
  ],
  image_gen_models: [
    {
      name: 'image',
      provider: 'jimeng',
      api_key: '',
      secret_key: '',
      model: 'jimeng_t2i_v31',
      base_url: '',
    },
  ],
  mcp_servers: {},
  jev: { api_key: 'test-key', model: 'jev-test', keep_threshold: 0.4 },
}

vi.mock('../../../wailsjs/go/main/App', () => ({
  GetConfig: vi.fn(() => Promise.resolve(config)),
  GetSkillManagement: vi.fn(() => Promise.resolve({ skills: [], plugins: [], mcp: [{ name: 'mcp-1', url: 'https://example.com/mcp', pluginEnabled: true, status: 'auth_required' }] })),
  MCPAuthorizationAction: vi.fn(() => Promise.resolve()),
  SaveConfig: vi.fn((cfg: ConfigView) => Promise.resolve(cfg)),
  ResolveModelProfile: vi.fn((model: ModelConfig) => Promise.resolve(
    model.context_window
      ? { context_window: model.context_window, context_window_source: 'override', reasoning_efforts: ['minimal', 'low', 'medium', 'high'] }
      : model.model.includes('deepseek')
        ? { context_window: 1048576, context_window_source: 'model', reasoning_efforts: ['none', 'high', 'max'] }
        : { context_window: 400000, context_window_source: 'model', reasoning_efforts: ['minimal', 'low', 'medium', 'high'] },
  )),
}))

vi.mock('../../../wailsjs/runtime/runtime', () => ({
  EventsOn: vi.fn(() => () => {}),
  Quit: vi.fn(),
}))

describe('ConfigPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    ;(window as unknown as { go?: unknown }).go = {}
  })

  afterEach(() => {
    delete (window as unknown as { go?: unknown }).go
  })

  it('saves a remote MCP before starting OAuth and preserves advanced settings', async () => {
    const user = userEvent.setup()
    render(<ConfigPanel open initialTab="mcp" onClose={vi.fn()} onSaved={vi.fn()} />)
    await user.click(await screen.findByRole('button', { name: '添加 MCP 服务' }))
    await user.selectOptions(screen.getByLabelText('连接方式'), 'http')
    await user.type(screen.getByLabelText('MCP URL'), 'https://example.com/mcp')
    await user.click(screen.getByText('OAuth 高级设置'))
    await user.type(screen.getByLabelText('预注册 Client ID（可选）'), 'public-client')
    await user.click(screen.getByRole('button', { name: '保存并授权' }))
    await waitFor(() => expect(MCPAuthorizationAction).toHaveBeenCalledWith('mcp-1', 'login'))
    const saved = vi.mocked(SaveConfig).mock.calls[0][0] as unknown as ConfigView
    expect(saved.mcp_servers?.['mcp-1']).toMatchObject({ url: 'https://example.com/mcp', command: '', oauth_client_id: 'public-client' })
    expect(vi.mocked(SaveConfig).mock.invocationCallOrder[0]).toBeLessThan(vi.mocked(MCPAuthorizationAction).mock.invocationCallOrder[0])
  })

  it('keeps focus while editing model name', async () => {
    const user = userEvent.setup()
    render(<ConfigPanel open initialTab="models" onClose={vi.fn()} onSaved={vi.fn()} />)

    const input = await screen.findByDisplayValue('main')
    await user.click(input)
    await user.type(input, '-dev')

    await waitFor(() => expect(input).toHaveValue('main-dev'))
    expect(document.activeElement).toBe(input)
    await user.click(screen.getByRole('button', { name: '保存配置' }))
    await waitFor(() => expect(SaveConfig).toHaveBeenCalled())
    const saved = vi.mocked(SaveConfig).mock.calls[0][0] as unknown as ConfigView
    expect(saved.active).toBe('main-dev')
    expect(saved.jev).toEqual(config.jev)
  })

  it('resolves model defaults after the model id changes', async () => {
    const user = userEvent.setup()
    render(<ConfigPanel open initialTab="models" onClose={vi.fn()} onSaved={vi.fn()} />)

    const modelID = await screen.findByDisplayValue('gpt-5')
    await user.clear(modelID)
    await user.type(modelID, 'deepseek-v4-flash')

    await waitFor(() => expect(ResolveModelProfile).toHaveBeenLastCalledWith(expect.objectContaining({ model: 'deepseek-v4-flash' })))
    expect(ResolveModelProfile).toHaveBeenLastCalledWith(expect.not.objectContaining({ api_key: expect.anything() }))
    await waitFor(() => expect(screen.getByPlaceholderText('自动 · 1,048,576')).toBeInTheDocument())
    expect(screen.getByText('Auto（模型默认）')).toBeInTheDocument()
  })

  it('persists only an explicit per-model context override', async () => {
    const user = userEvent.setup()
    render(<ConfigPanel open initialTab="models" onClose={vi.fn()} onSaved={vi.fn()} />)

    const override = await screen.findByPlaceholderText('自动 · 400,000')
    await user.type(override, '96000')
    const save = screen.getByRole('button', { name: '保存配置' })
    await waitFor(() => expect(save).toBeEnabled())
    await user.click(save)

    await waitFor(() => expect(SaveConfig).toHaveBeenCalled())
    const saved = vi.mocked(SaveConfig).mock.calls[0][0] as unknown as ConfigView
    expect(saved.models[0].context_window).toBe(96000)
    expect(saved.models[0].profile).toBeUndefined()
  })
})
