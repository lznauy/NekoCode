import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ConfigPanel } from '../ConfigPanel'
import type { ConfigView, ModelConfig } from '../../types/config'
import { GetConfig, ResolveModelProfile, SaveConfig, MCPAuthorizationAction } from '../../../wailsjs/go/main/App'

const config: ConfigView = {
  path: '/tmp/config.json',
  exists: true,
  active: 'openai/main',
  auto_compact_percent: 80,
  providers: [{ name: 'openai', api_key: '', protocol: 'openai', models: [{ name: 'main', model: 'gpt-5', reasoning_effort: 'medium' }] }],
  models: [
    {
      name: 'openai/main',
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
  image_understand_models: [],
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
    expect(saved.active).toBe('openai/main-dev')
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
    expect(saved.providers[0].models[0]).toMatchObject({ context_window: 96000 })
    expect(saved.providers[0].models[0]).not.toHaveProperty('profile')
  })

  it('adds and saves an image understanding model', async () => {
    const user = userEvent.setup()
    render(<ConfigPanel open initialTab="models" onClose={vi.fn()} onSaved={vi.fn()} />)

    await user.click(await screen.findByRole('button', { name: '添加理解模型' }))
    const modelIDs = screen.getAllByLabelText('模型 ID')
    await user.type(modelIDs[modelIDs.length - 1], 'gpt-5-mini')
    await user.click(screen.getByRole('button', { name: '保存配置' }))

    await waitFor(() => expect(SaveConfig).toHaveBeenCalled())
    const saved = vi.mocked(SaveConfig).mock.calls[0][0] as unknown as ConfigView
    expect(saved.image_understand_models).toEqual([
      expect.objectContaining({ name: 'vision-1', protocol: 'openai', model: 'gpt-5-mini' }),
    ])
  })

  it('shares connection settings, preserves shorthand and updates both selections when a provider is renamed', async () => {
    vi.mocked(GetConfig).mockResolvedValueOnce({ ...config,
      active: 'relay/model-a', compaction_model: 'relay/model-b',
      providers: [{ name: 'relay', base_url: 'https://example.com/v1', api_key: 'shared-key', protocol: 'openai', models: ['model-a', 'model-b'] }],
      models: ['model-a', 'model-b'].map((model) => ({ ...config.models[0], name: `relay/${model}`, model })),
    } as never)
    const user = userEvent.setup()
    render(<ConfigPanel open initialTab="models" onClose={vi.fn()} onSaved={vi.fn()} />)
    const providerName = await screen.findByLabelText('服务名称')
    expect(screen.getAllByLabelText('API Key')).toHaveLength(1)
    await user.clear(providerName)
    await user.type(providerName, 'gateway')
    await user.clear(screen.getByLabelText('API Key'))
    await user.type(screen.getByLabelText('API Key'), 'replacement-key')
    const save = screen.getByRole('button', { name: '保存配置' })
    await waitFor(() => expect(save).toBeEnabled())
    await user.click(save)
    await waitFor(() => expect(SaveConfig).toHaveBeenCalled())
    const saved = vi.mocked(SaveConfig).mock.calls[0][0] as unknown as ConfigView
    expect(saved.providers).toEqual([{ name: 'gateway', base_url: 'https://example.com/v1', api_key: 'replacement-key', protocol: 'openai', models: ['model-a', 'model-b'] }])
    expect(saved.active).toBe('gateway/model-a')
    expect(saved.compaction_model).toBe('gateway/model-b')
  })

  it('keeps explicit protocol and vision overrides when the provider protocol changes', async () => {
    vi.mocked(GetConfig).mockResolvedValueOnce({ ...config,
      providers: [{ ...config.providers[0], models: [{ name: 'main', model: 'gpt-5', protocol: 'openai', vision: false }] }],
    } as never)
    const user = userEvent.setup()
    render(<ConfigPanel open initialTab="models" onClose={vi.fn()} onSaved={vi.fn()} />)
    await user.click(await screen.findByLabelText('默认协议'))
    await user.click(screen.getByRole('button', { name: 'anthropic' }))
    const save = screen.getByRole('button', { name: '保存配置' })
    await waitFor(() => expect(save).toBeEnabled())
    await user.click(save)
    await waitFor(() => expect(SaveConfig).toHaveBeenCalled())
    const saved = vi.mocked(SaveConfig).mock.calls[0][0] as unknown as ConfigView
    expect(saved.providers[0].protocol).toBe('anthropic')
    expect(saved.providers[0].models[0]).toMatchObject({ protocol: 'openai', vision: false })
  })

  it('rejects duplicate aliases inside a provider', async () => {
    const user = userEvent.setup()
    render(<ConfigPanel open initialTab="models" onClose={vi.fn()} onSaved={vi.fn()} />)
    await user.click(await screen.findByRole('button', { name: '添加模型' }))
    await user.type(screen.getAllByLabelText('模型 ID')[1], 'model-b')
    await user.type(screen.getAllByLabelText('模型别名（可选）')[1], 'main')
    expect(screen.getByText('模型名称重复：openai/main')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '保存配置' })).toBeDisabled()
  })
  it('changing from inherited protocol to the identical explicit protocol remains saveable', async () => {
    const user = userEvent.setup()
    render(<ConfigPanel open initialTab="models" onClose={vi.fn()} onSaved={vi.fn()} />)
    const save = await screen.findByRole('button', { name: '保存配置' })
    await waitFor(() => expect(save).toBeEnabled())
    // Let the initial metadata request finish before changing the override.
    await waitFor(() => expect(ResolveModelProfile).toHaveBeenCalled())
    await user.click(screen.getByLabelText('协议'))
    await user.click(screen.getAllByRole('button', { name: 'openai' }).slice(-1)[0])
    await waitFor(() => expect(save).toBeEnabled())
  })

  it('a temporary duplicate provider name does not switch the active connection', async () => {
    vi.mocked(GetConfig).mockResolvedValueOnce({ ...config,
      active: 'first/main', compaction_model: 'first/main',
      providers: ['first', 'second'].map((name) => ({ name, api_key: `${name}-secret`, protocol: 'openai', models: [{name: 'main', model: 'gpt-5'}] })),
      models: ['first', 'second'].map((name) => ({ ...config.models[0], name: `${name}/main`, provider: name })),
    } as never)
    const user = userEvent.setup()
    render(<ConfigPanel open initialTab="models" onClose={vi.fn()} onSaved={vi.fn()} />)
    await screen.findAllByLabelText('服务名称')
    const input = screen.getAllByLabelText('服务名称')[0]
    await user.clear(input)
    await user.type(input, 'second')
    await user.clear(input)
    await user.type(input, 'first')
    const save = screen.getByRole('button', { name: '保存配置' })
    await waitFor(() => expect(save).toBeEnabled())
    await user.click(save)
    await waitFor(() => expect(SaveConfig).toHaveBeenCalled())
    const saved = vi.mocked(SaveConfig).mock.calls[0][0] as unknown as ConfigView
    expect(saved.active).toBe('first/main')
    expect(saved.compaction_model).toBe('first/main')
  })

  it('changing an explicit protocol back to the identical inherited protocol remains saveable', async () => {
    vi.mocked(GetConfig).mockResolvedValueOnce({ ...config,
      providers: [{ ...config.providers[0], models: [{name: 'main', model: 'gpt-5', protocol: 'openai'}] }],
    } as never)
    const user = userEvent.setup()
    render(<ConfigPanel open initialTab="models" onClose={vi.fn()} onSaved={vi.fn()} />)
    await waitFor(() => expect(ResolveModelProfile).toHaveBeenCalled())
    await user.click(screen.getByLabelText('协议'))
    await user.click(screen.getByRole('button', {name: '跟随服务'}))
    const save = screen.getByRole('button', {name: '保存配置'})
    await waitFor(() => expect(save).toBeEnabled())
    await user.click(save)
    await waitFor(() => expect(SaveConfig).toHaveBeenCalled())
    const saved = vi.mocked(SaveConfig).mock.calls[0][0] as unknown as ConfigView
    expect(saved.providers[0].models[0]).not.toHaveProperty('protocol', 'openai')
  })

  it('a temporary duplicate model alias keeps the selected row', async () => {
    vi.mocked(GetConfig).mockResolvedValueOnce({ ...config,
      active: 'openai/first', compaction_model: 'openai/first',
      providers: [{ ...config.providers[0], models: ['first', 'second'].map((name) => ({name, model: 'gpt-5'})) }],
      models: ['first', 'second'].map((name) => ({ ...config.models[0], name: `openai/${name}` })),
    } as never)
    const user = userEvent.setup()
    render(<ConfigPanel open initialTab="models" onClose={vi.fn()} onSaved={vi.fn()} />)
    const input = (await screen.findAllByLabelText('模型别名（可选）'))[0]
    await user.clear(input)
    await user.type(input, 'second')
    await user.clear(input)
    await user.type(input, 'first')
    const save = screen.getByRole('button', {name: '保存配置'})
    await waitFor(() => expect(save).toBeEnabled())
    await user.click(save)
    await waitFor(() => expect(SaveConfig).toHaveBeenCalled())
    const saved = vi.mocked(SaveConfig).mock.calls[0][0] as unknown as ConfigView
    expect(saved.active).toBe('openai/first')
    expect(saved.compaction_model).toBe('openai/first')
  })

  it('retains manual selections after deleting an earlier provider and excludes internal row keys from saves', async () => {
    vi.mocked(GetConfig).mockResolvedValueOnce({ ...config,
      active: 'first/main', compaction_model: '',
      providers: ['first', 'second'].map((name) => ({ name, protocol: 'openai', models: [{name: 'main', model: 'gpt-5'}] })),
      models: ['first', 'second'].map((name) => ({ ...config.models[0], name: `${name}/main`, provider: name })),
    } as never)
    const user = userEvent.setup()
    render(<ConfigPanel open onClose={vi.fn()} onSaved={vi.fn()} />)
    await user.click(await screen.findByLabelText('当前模型'))
    await user.click(screen.getByRole('button', {name: 'second/main'}))
    await user.click(screen.getByLabelText('上下文压缩模型'))
    await user.click(screen.getAllByRole('button', {name: 'second/main'}).slice(-1)[0])
    await user.click(screen.getByRole('button', {name: '模型'}))
    await user.click(screen.getAllByRole('button', {name: '删除服务'})[0])
    const save = screen.getByRole('button', {name: '保存配置'})
    await waitFor(() => expect(save).toBeEnabled())
    await user.click(save)
    await waitFor(() => expect(SaveConfig).toHaveBeenCalled())
    const saved = vi.mocked(SaveConfig).mock.calls[0][0] as unknown as ConfigView
    expect(saved.active).toBe('second/main')
    expect(saved.compaction_model).toBe('second/main')
    expect(saved.providers).toHaveLength(1)
    expect(saved.providers[0].name).toBe('second')
    expect(saved).not.toHaveProperty('activeModelKey')
    expect(saved).not.toHaveProperty('compactionModelKey')
    expect(saved.providers[0]).not.toHaveProperty('uiKey')
  })
})
