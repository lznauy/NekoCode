import { useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { cn } from '../lib/classnames'
import { isWailsEnvironment, safeGetConfig, safeResolveModelProfile, safeSaveConfig, safeSkillManagementView, safeMCPAuthorizationAction } from '../lib/wails'
import type { ConfigView, ImageGenConfig, MCPServerConfig, ModelConfig } from '../types/config'
import type { SkillManagementView } from '../types/skills'
import { Select } from './Select'

interface ConfigPanelProps {
  open: boolean
  onClose: () => void
  onSaved: () => void
  initialTab?: ConfigTab
}

const emptyModel = (name: string): ModelConfig => ({
  name,
  provider: 'openai',
  api_key: '',
  model: '',
  base_url: '',
  protocol: 'openai',
  reasoning_effort: '',
  context_window: 0,
})

type EditableModelConfig = ModelConfig & { uiKey: string }
type EditableImageGenConfig = ImageGenConfig & { uiKey: string }
type EditableConfigView = Omit<ConfigView, 'models' | 'image_gen_models'> & {
  models: EditableModelConfig[]
  image_gen_models?: EditableImageGenConfig[]
}

const emptyImageModel = (name: string): ImageGenConfig => ({
  name,
  provider: 'jimeng',
  api_key: '',
  secret_key: '',
  base_url: 'https://visual.volcengineapi.com',
  model: 'jimeng_t2i_v31',
})

const emptyMcpServer = (): MCPServerConfig => ({
  command: '',
  args: [],
  env: {},
  enabled: true,
})

export type ConfigTab = 'overview' | 'models' | 'mcp'

const configTabs: Array<{ value: ConfigTab; label: string }> = [
  { value: 'overview', label: '概览' },
  { value: 'models', label: '模型' },
  { value: 'mcp', label: 'MCP 服务' },
]

let configRowId = 0

function nextConfigRowKey(prefix: string) {
  configRowId += 1
  return `${prefix}-${configRowId}`
}

function withModelKey(model: ModelConfig): EditableModelConfig {
  return { ...model, uiKey: nextConfigRowKey('model') }
}

function withModelKeys(models: ModelConfig[]): EditableModelConfig[] {
  return models.map(withModelKey)
}

function withImageModelKey(model: ImageGenConfig): EditableImageGenConfig {
  return { ...model, uiKey: nextConfigRowKey('image') }
}

function withImageModelKeys(models: ImageGenConfig[]): EditableImageGenConfig[] {
  return models.map(withImageModelKey)
}

export function ConfigPanel({ open, onClose, onSaved, initialTab = 'overview' }: ConfigPanelProps) {
  const [cfg, setCfg] = useState<EditableConfigView | null>(null)
  const [tab, setTab] = useState<ConfigTab>(initialTab)
  const [selectedMcp, setSelectedMcp] = useState('')
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [saved, setSaved] = useState(false)
  const mcpHealth = useMCPHealth(open && tab === 'mcp')

  useEffect(() => {
    if (!open) return
    setTab(initialTab)
    setLoading(true)
    setError('')
    setSaved(false)
    if (!isWailsEnvironment()) {
      setCfg(null)
      setError('当前是浏览器预览环境，无法访问 Wails 配置接口；请通过 Wails GUI 运行后再打开配置管理。')
      setLoading(false)
      return
    }
    safeGetConfig()
      .then((next) => {
        if (!next) {
          setError('无法读取配置：Wails 配置接口没有返回数据')
          return
        }
        setCfg({
          ...next,
          models: withModelKeys(next.models ?? []),
          image_gen_models: withImageModelKeys(next.image_gen_models ?? []),
          mcp_servers: next.mcp_servers ?? {},
        })
        setSelectedMcp(Object.keys(next.mcp_servers ?? {})[0] ?? '')
      })
      .catch((err: unknown) => setError(err instanceof Error ? err.message : String(err)))
      .finally(() => setLoading(false))
  }, [initialTab, open])

  const validation = useMemo(() => validateConfig(cfg), [cfg])
  const mcpEntries = Object.entries(cfg?.mcp_servers ?? {})
  const enabledMcpCount = mcpEntries.filter(([, srv]) => srv.enabled).length
  const selectedMcpEntry = mcpEntries.find(([name]) => name === selectedMcp) ?? mcpEntries[0]
  const activeModel = cfg?.models.find((model) => model.name === cfg.active)

  if (!open) return null

  const update = (patch: Partial<EditableConfigView>) => {
    setSaved(false)
    setCfg((prev) => (prev ? { ...prev, ...patch } : prev))
  }

  const updateModel = (idx: number, patch: Partial<ModelConfig>) => {
    setSaved(false)
    setCfg((prev) => {
      if (!prev) return prev
      const profileChanged = 'provider' in patch || 'model' in patch || 'protocol' in patch || 'context_window' in patch
      const models = prev.models.map((m, i) => i === idx ? { ...m, ...patch, ...(profileChanged ? { profile: undefined } : {}) } : m)
      const next: EditableConfigView = { ...prev, models }
      const previousName = prev.models[idx]?.name
      if (previousName && next.active === previousName) next.active = models[idx].name
      if (previousName && next.flash_model === previousName) next.flash_model = models[idx].name
      if (!models.some((m) => m.name === next.active)) next.active = models[0]?.name ?? ''
      if (next.flash_model && !models.some((m) => m.name === next.flash_model)) next.flash_model = ''
      return next
    })
  }

  const updateImageModel = (idx: number, patch: Partial<ImageGenConfig>) => {
    setSaved(false)
    setCfg((prev) => {
      if (!prev) return prev
      const image_gen_models = (prev.image_gen_models ?? []).map((m, i) => (i === idx ? { ...m, ...patch } : m))
      return { ...prev, image_gen_models }
    })
  }

  const addModel = () => {
    setCfg((prev) => {
      if (!prev) return prev
      const name = nextName(prev.models.map((m) => m.name), 'model')
      return { ...prev, models: [...prev.models, withModelKey(emptyModel(name))], active: prev.active || name }
    })
    setSaved(false)
  }

  const removeModel = (idx: number) => {
    setCfg((prev) => {
      if (!prev || prev.models.length <= 1) return prev
      const removed = prev.models[idx]
      const models = prev.models.filter((_, i) => i !== idx)
      return {
        ...prev,
        models,
        active: prev.active === removed.name ? models[0].name : prev.active,
        flash_model: prev.flash_model === removed.name ? '' : prev.flash_model,
      }
    })
    setSaved(false)
  }

  const addImageModel = () => {
    setCfg((prev) => {
      if (!prev) return prev
      const names = (prev.image_gen_models ?? []).map((m) => m.name)
      return { ...prev, image_gen_models: [...(prev.image_gen_models ?? []), withImageModelKey(emptyImageModel(nextName(names, 'image')))] }
    })
    setSaved(false)
  }

  const removeImageModel = (idx: number) => {
    setCfg((prev) => {
      if (!prev) return prev
      return { ...prev, image_gen_models: (prev.image_gen_models ?? []).filter((_, i) => i !== idx) }
    })
    setSaved(false)
  }

  const addMcpServer = () => {
    setCfg((prev) => {
      if (!prev) return prev
      const name = nextName(Object.keys(prev.mcp_servers ?? {}), 'mcp')
      setSelectedMcp(name)
      return {
        ...prev,
        mcp_servers: {
          ...(prev.mcp_servers ?? {}),
          [name]: emptyMcpServer(),
        },
      }
    })
    setSaved(false)
    setTab('mcp')
  }

  const renameMcpServer = (oldName: string, nextNameValue: string) => {
    setCfg((prev) => {
      if (!prev) return prev
      const servers = { ...(prev.mcp_servers ?? {}) }
      const value = servers[oldName]
      delete servers[oldName]
      servers[nextNameValue] = value
      setSelectedMcp(nextNameValue)
      return { ...prev, mcp_servers: servers }
    })
    setSaved(false)
  }

  const updateMcpServer = (name: string, patch: Partial<MCPServerConfig>) => {
    setCfg((prev) => {
      if (!prev) return prev
      const current = prev.mcp_servers?.[name] ?? emptyMcpServer()
      return {
        ...prev,
        mcp_servers: {
          ...(prev.mcp_servers ?? {}),
          [name]: { ...current, ...patch },
        },
      }
    })
    setSaved(false)
  }

  const removeMcpServer = (name: string) => {
    setCfg((prev) => {
      if (!prev) return prev
      const servers = { ...(prev.mcp_servers ?? {}) }
      delete servers[name]
      if (selectedMcp === name) {
        setSelectedMcp(Object.keys(servers)[0] ?? '')
      }
      return { ...prev, mcp_servers: servers }
    })
    setSaved(false)
  }

  const save = async () => {
    if (!cfg || validation) return
    setSaving(true)
    setError('')
    setSaved(false)
    try {
      const savedCfg = await safeSaveConfig({
        ...cfg,
        models: cfg.models.map((model) => trimModel(model)),
        image_gen_models: (cfg.image_gen_models ?? []).map((model) => trimImageModel(model)),
        mcp_servers: trimMcpServers(cfg.mcp_servers ?? {}),
      })
      if (!savedCfg) {
        setError('保存失败：Wails 配置接口没有返回数据')
        return
      }
      setCfg({
        ...savedCfg,
        models: withModelKeys(savedCfg.models ?? []),
        image_gen_models: withImageModelKeys(savedCfg.image_gen_models ?? []),
        mcp_servers: savedCfg.mcp_servers ?? {},
      })
      setSaved(true)
      onSaved()
      return savedCfg
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="fixed inset-0 z-40 flex justify-end bg-black/35" onMouseDown={onClose}>
      <aside
        className="flex h-full w-full max-w-[760px] flex-col border-l border-border/70 bg-surface-2 surface-shadow animate-slide-in"
        onMouseDown={(e) => e.stopPropagation()}
      >
        <header className="flex min-h-[56px] items-center gap-3 border-b border-border/60 px-5">
          <div className="flex h-8 w-8 items-center justify-center rounded-md bg-primary/15 text-primary">
            <GearIcon />
          </div>
          <div className="min-w-0 flex-1">
            <h2 className="text-sm font-semibold leading-tight text-text">配置管理</h2>
            <p className="mt-0.5 truncate text-[11px] text-text-3">{cfg?.path || '读取配置文件...'}</p>
          </div>
          <button className="icon-button tooltip-anchor" type="button" data-tooltip="关闭" data-tooltip-align="end" aria-label="关闭配置" onClick={onClose}>
            <CloseIcon />
          </button>
        </header>

        <nav className="flex gap-1 border-b border-border/60 px-5 py-2">
          {configTabs.map((option) => (
            <button
              key={option.value}
              type="button"
              className={cn(
                'inline-flex h-8 items-center gap-1.5 rounded-md px-3 text-xs font-semibold transition-all active:scale-95',
                tab === option.value ? 'bg-primary text-black' : 'bg-surface-3 text-text-2 hover:text-text',
              )}
              onClick={() => setTab(option.value)}
            >
              {option.label}
            </button>
          ))}
        </nav>

        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
          {loading && <div className="text-sm text-text-2">正在识别配置文件...</div>}
          {!loading && cfg && (
            <div className="space-y-4">
              {tab === 'overview' && (
                <>
              <section className="rounded-md border border-border/50 bg-surface px-4 py-3">
                <div className="flex flex-wrap items-center gap-2">
                  <StatusPill ok={cfg.exists} text={cfg.exists ? '已识别配置文件' : '未找到配置文件，保存后创建'} />
                  <span className="text-[11px] text-text-3">{cfg.models.length} 个文本模型</span>
                  <span className="text-[11px] text-text-3">{cfg.image_gen_models?.length ?? 0} 个图片模型</span>
                  <span className="text-[11px] text-text-3">{mcpEntries.length} 个 MCP 服务</span>
                </div>
              </section>

              <section className="grid gap-3 rounded-md border border-border/50 bg-surface px-4 py-3 md:grid-cols-4">
                <Field label="当前模型">
                  <Select
                    value={cfg.active}
                    options={cfg.models.map((m) => ({ value: m.name, label: m.name }))}
                    onChange={(v) => update({ active: v })}
                  />
                </Field>
                <Field label="Flash 模型">
                  <Select
                    value={cfg.flash_model ?? ''}
                    options={[{ value: '', label: '跟随当前模型' }, ...cfg.models.map((m) => ({ value: m.name, label: m.name }))]}
                    onChange={(v) => update({ flash_model: v })}
                  />
                </Field>
                <Field label="当前有效窗口">
                  <div className="field flex items-center justify-between bg-surface-3/70 text-text-2">
                    <span>{formatNumber(activeModel?.profile?.context_window ?? 0)}</span>
                    <span className="text-[10px] text-text-3">{contextWindowSource(activeModel?.profile?.context_window_source)}</span>
                  </div>
                </Field>
                <Field label="自动压缩门限 (%)">
                  <input
                    className="field"
                    inputMode="numeric"
                    min={1}
                    max={99}
                    value={String(cfg.auto_compact_percent || '')}
                    onChange={(e) => update({ auto_compact_percent: Number(e.target.value) || 0 })}
                  />
                </Field>
              </section>
              <section className="grid gap-2 md:grid-cols-3">
                <ConfigShortcut title="文本模型" detail={`${cfg.models.length} 个，当前 ${cfg.active || '未设置'}`} onClick={() => setTab('models')} />
                <ConfigShortcut title="图片模型" detail={`${cfg.image_gen_models?.length ?? 0} 个可用配置`} onClick={() => setTab('models')} />
                <ConfigShortcut title="MCP 服务" detail={`${enabledMcpCount}/${mcpEntries.length} 已启用`} onClick={() => setTab('mcp')} />
              </section>
                </>
              )}

              {tab === 'models' && (
                <>
              <section>
                <SectionTitle title="文本模型" action="添加模型" onAction={addModel} />
                <div className="mt-2 space-y-2">
                  {cfg.models.map((model, idx) => (
                    <ModelCard
                      key={model.uiKey}
                      model={model}
                      active={cfg.active === model.name}
                      canRemove={cfg.models.length > 1}
                      onChange={(patch) => updateModel(idx, patch)}
                      onRemove={() => removeModel(idx)}
                    />
                  ))}
                </div>
              </section>

              <section>
                <SectionTitle title="图片模型" action="添加图片模型" onAction={addImageModel} />
                <div className="mt-2 space-y-2">
                  {(cfg.image_gen_models ?? []).map((model, idx) => (
                    <ImageModelCard
                      key={model.uiKey}
                      model={model}
                      onChange={(patch) => updateImageModel(idx, patch)}
                      onRemove={() => removeImageModel(idx)}
                    />
                  ))}
                  {(cfg.image_gen_models ?? []).length === 0 && (
                    <div className="rounded-md border border-dashed border-border/70 px-4 py-6 text-center text-xs text-text-3">
                      暂无图片模型
                    </div>
                  )}
                </div>
              </section>
                </>
              )}

              {tab === 'mcp' && (
                <section>
                  <SectionTitle title="MCP 服务" action="添加 MCP 服务" onAction={addMcpServer} />
                  <div className="mt-2 grid min-h-[360px] gap-3 md:grid-cols-[220px_minmax(0,1fr)]">
                    <div className="rounded-md border border-border/50 bg-surface p-2">
                      {mcpEntries.map(([name, server]) => (
                        <button
                          key={name}
                          type="button"
                          className={cn(
                            'mb-1 grid w-full grid-cols-[auto_minmax(0,1fr)] items-center gap-2 rounded-md px-2 py-2 text-left transition-all last:mb-0 active:scale-[0.99]',
                            selectedMcpEntry?.[0] === name ? 'bg-primary/14 text-text' : 'text-text-2 hover:bg-surface-3 hover:text-text',
                          )}
                          onClick={() => setSelectedMcp(name)}
                        >
                          <span className={cn('h-2 w-2 rounded-full', server.enabled ? 'bg-success' : 'bg-text-3')} />
                          <span className="min-w-0">
                            <span className="block truncate text-xs font-semibold">{name}</span>
                            <span className="mt-0.5 block truncate font-mono text-[10px] text-text-3">{server.command || '未配置 command'}</span>
                          </span>
                        </button>
                      ))}
                    </div>
                    {selectedMcpEntry && (
                      <McpServerCard
                        key={selectedMcpEntry[0]}
                        name={selectedMcpEntry[0]}
                        server={selectedMcpEntry[1]}
                        onRename={(nextNameValue) => renameMcpServer(selectedMcpEntry[0], nextNameValue)}
                        onChange={(patch) => updateMcpServer(selectedMcpEntry[0], patch)}
                        onRemove={() => removeMcpServer(selectedMcpEntry[0])}
                        health={mcpHealth?.mcp.find((s) => s.name === selectedMcpEntry[0].trim() && s.url === selectedMcpEntry[1].url?.trim() && s.status !== 'shadowed')}
                        onAuthorize={async (action) => {
                          if (action === 'login' && !await save()) throw new Error(validation || '请先完成并保存配置')
                          const live = await safeSkillManagementView()
                          const liveServer = live?.mcp.find((server) => server.name === selectedMcpEntry[0].trim() && server.url === selectedMcpEntry[1].url?.trim())
                          if (!liveServer) throw new Error('当前生效的服务与此配置不同，请先保存配置或检查同名定义')
                          if (liveServer.status === 'disabled') throw new Error('该服务已停用，请先勾选「启用」并保存')
                          if (!liveServer.pluginEnabled || liveServer.status === 'shadowed') throw new Error('当前生效的服务与此配置不同，请检查同名项目配置或插件')
                          await safeMCPAuthorizationAction(selectedMcpEntry[0].trim(), action)
                        }}
                      />
                    )}
                    {mcpEntries.length === 0 && (
                      <div className="rounded-md border border-dashed border-border/70 bg-surface px-4 py-8 text-center text-xs text-text-3 md:col-span-2">
                        暂无 MCP 服务配置
                      </div>
                    )}
                  </div>
                </section>
              )}
            </div>
          )}
        </div>

        <footer className="flex min-h-[60px] items-center gap-3 border-t border-border/60 px-5">
          <div className="min-w-0 flex-1 text-xs">
            {(error || validation) && <span className="text-danger">{error || validation}</span>}
            {saved && !error && !validation && <span className="text-success">已保存并应用</span>}
          </div>
          <button type="button" className="secondary-button" onClick={onClose}>取消</button>
          <button type="button" className="primary-button" disabled={!cfg || !!validation || saving} onClick={save}>
            {saving ? '保存中...' : '保存配置'}
          </button>
        </footer>
      </aside>
    </div>
  )
}

function ModelCard({
  model,
  active,
  canRemove,
  onChange,
  onRemove,
}: {
  model: ModelConfig
  active: boolean
  canRemove: boolean
  onChange: (patch: Partial<ModelConfig>) => void
  onRemove: () => void
}) {
  useEffect(() => {
    let cancelled = false
    const timer = window.setTimeout(() => {
      safeResolveModelProfile(model).then((profile) => {
        if (cancelled || !profile) return
        const effort = model.reasoning_effort || ''
        const supported = effort === '' || (profile.reasoning_efforts ?? []).includes(effort)
        onChange({ profile, reasoning_effort: supported ? model.reasoning_effort : '' })
      })
    }, 250)
    return () => {
      cancelled = true
      window.clearTimeout(timer)
    }
  }, [model.context_window, model.model, model.protocol, model.provider])

  return (
    <div className="rounded-md border border-border/50 bg-surface px-4 py-3">
      <div className="mb-3 flex items-center gap-2">
        <span className={cn('h-2 w-2 rounded-full', active ? 'bg-primary' : 'bg-text-3')} />
        <span className="min-w-0 flex-1 truncate text-xs font-semibold text-text">{model.name || '未命名模型'}</span>
        <button className="danger-button" type="button" disabled={!canRemove} onClick={onRemove}>删除</button>
      </div>
      <div className="grid gap-3 md:grid-cols-2">
        <Field label="名称"><input className="field" value={model.name} onChange={(e) => onChange({ name: e.target.value })} /></Field>
        <Field label="Provider"><input className="field" value={model.provider} onChange={(e) => onChange({ provider: e.target.value })} /></Field>
        <Field label="模型 ID"><input className="field" value={model.model} onChange={(e) => onChange({ model: e.target.value })} /></Field>
        <Field label="协议">
          <Select
            value={model.protocol || 'openai'}
            options={[{ value: 'openai', label: 'openai' }, { value: 'anthropic', label: 'anthropic' }]}
            onChange={(v) => onChange({ protocol: v as ModelConfig['protocol'] })}
          />
        </Field>
        <Field label="推理强度">
          <Select
            value={model.reasoning_effort || ''}
            options={[
              { value: '', label: 'Auto（模型默认）' },
              ...(model.profile?.reasoning_efforts || []).map((value) => ({ value, label: value === 'none' ? 'Off' : value })),
            ]}
            onChange={(v) => onChange({ reasoning_effort: v as ModelConfig['reasoning_effort'] })}
          />
        </Field>
        <Field label="上下文窗口覆盖">
          <div>
            <input
              className="field font-mono"
              inputMode="numeric"
              min={1}
              placeholder={`自动 · ${formatNumber(model.profile?.context_window ?? 0)}`}
              value={model.context_window ? String(model.context_window) : ''}
              onChange={(e) => onChange({ context_window: Number(e.target.value) || 0 })}
            />
            <span className="mt-1 block text-[10px] text-text-3">
              {model.context_window ? '使用自定义覆盖值' : `自动解析 · ${contextWindowSource(model.profile?.context_window_source)}`}
            </span>
          </div>
        </Field>
        <Field label="API Key"><input className="field font-mono" type="password" value={model.api_key} onChange={(e) => onChange({ api_key: e.target.value })} /></Field>
        <Field label="Base URL"><input className="field font-mono" value={model.base_url ?? ''} onChange={(e) => onChange({ base_url: e.target.value })} /></Field>
      </div>
    </div>
  )
}

function ImageModelCard({
  model,
  onChange,
  onRemove,
}: {
  model: ImageGenConfig
  onChange: (patch: Partial<ImageGenConfig>) => void
  onRemove: () => void
}) {
  return (
    <div className="rounded-md border border-border/50 bg-surface px-4 py-3">
      <div className="mb-3 flex items-center gap-2">
        <span className="min-w-0 flex-1 truncate text-xs font-semibold text-text">{model.name || '未命名图片模型'}</span>
        <button className="danger-button" type="button" onClick={onRemove}>删除</button>
      </div>
      <div className="grid gap-3 md:grid-cols-2">
        <Field label="名称"><input className="field" value={model.name} onChange={(e) => onChange({ name: e.target.value })} /></Field>
        <Field label="Provider"><input className="field" value={model.provider} onChange={(e) => onChange({ provider: e.target.value })} /></Field>
        <Field label="模型 ID"><input className="field" value={model.model ?? ''} onChange={(e) => onChange({ model: e.target.value })} /></Field>
        <Field label="Base URL"><input className="field font-mono" value={model.base_url ?? ''} onChange={(e) => onChange({ base_url: e.target.value })} /></Field>
        <Field label="Access Key"><input className="field font-mono" type="password" value={model.api_key} onChange={(e) => onChange({ api_key: e.target.value })} /></Field>
        <Field label="Secret Key"><input className="field font-mono" type="password" value={model.secret_key} onChange={(e) => onChange({ secret_key: e.target.value })} /></Field>
      </div>
    </div>
  )
}

// Schedule after completion so slow requests cannot overlap or reorder status.
function useMCPHealth(enabled: boolean) {
  const [view, setView] = useState<SkillManagementView | null>()
  useEffect(() => {
    if (!enabled) return
    let active = true
    let timer: number | undefined
    const poll = async () => {
      try {
        const next = await safeSkillManagementView()
        if (active) setView(next)
      } catch { /* keep the last known status and retry */ }
      if (active) timer = window.setTimeout(poll, 1500)
    }
    void poll()
    return () => { active = false; window.clearTimeout(timer) }
  }, [enabled])
  return view
}

function McpServerCard({
  name,
  server,
  onRename,
  onChange,
  onRemove,
  onAuthorize,
  health,
}: {
  name: string
  server: MCPServerConfig
  onRename: (name: string) => void
  onAuthorize: (action: string) => Promise<void>
  health?: import('../types/skills').MCPServerView
  onChange: (patch: Partial<MCPServerConfig>) => void
  onRemove: () => void
}) {
  const [draftName, setDraftName] = useState(name)
  const [remote, setRemote] = useState(Boolean(server.url))
  const [authError, setAuthError] = useState('')
  const [authBusy, setAuthBusy] = useState(false)
  const authorize = async (action: string) => {
    setAuthBusy(true); setAuthError('')
    try { await onAuthorize(action) } catch (err) { setAuthError(String(err)) }
    finally { setAuthBusy(false) }
  }
  const statusLabels: Record<string, string> = { ready: '已连接', starting: '正在连接', auth_required: '需要授权', authorizing: '等待浏览器授权', error: '连接失败', disabled: '未启用' }

  useEffect(() => setDraftName(name), [name])
  const argsText = (server.args ?? []).join('\n')
  const envText = Object.entries(server.env ?? {})
    .map(([key, value]) => `${key}=${value}`)
    .join('\n')
  const commitName = () => {
    const next = draftName.trim()
    if (next && next !== name) onRename(next)
    else setDraftName(name)
  }

  return (
    <div className="rounded-md border border-border/50 bg-surface px-4 py-3">
      <div className="mb-3 flex items-center gap-2">
        <span className={cn('h-2 w-2 rounded-full', server.enabled ? 'bg-success' : 'bg-text-3')} />
        <span className="min-w-0 flex-1 truncate text-xs font-semibold text-text">{name || '未命名 MCP 服务'}</span>
        <label className="inline-flex h-7 items-center gap-1.5 rounded-md bg-surface-3 px-2 text-[11px] text-text-2">
          <input
            type="checkbox"
            className="h-3.5 w-3.5 accent-[var(--bl)]"
            checked={server.enabled}
            onChange={(e) => onChange({ enabled: e.target.checked })}
          />
          启用
        </label>
        <button className="danger-button" type="button" onClick={onRemove}>删除</button>
      </div>
      <div className="grid gap-3 md:grid-cols-2">
        <Field label="服务名称">
          <input
            className="field font-mono"
            value={draftName}
            onChange={(e) => setDraftName(e.target.value)}
            onBlur={commitName}
            onKeyDown={(e) => {
              if (e.key === 'Enter') e.currentTarget.blur()
            }}
          />
        </Field>
        <Field label="连接方式">
          <select className="field" value={remote ? 'http' : 'stdio'} onChange={(e) => {
            const isRemote = e.target.value === 'http'
            setRemote(isRemote)
            onChange(isRemote ? { command: '', args: [], env: {}, url: '' } : { url: '', oauth_client_id: '', oauth_client_secret: '', oauth_client_metadata_url: '', oauth_callback_port: 0 })
          }}>
            <option value="stdio">本地命令（stdio）</option>
            <option value="http">远程 URL（Streamable HTTP）</option>
          </select>
        </Field>
        {remote ? <>
          <Field label="MCP URL">
            <input className="field font-mono" placeholder="https://example.com/mcp" value={server.url ?? ''} onChange={(e) => onChange({ url: e.target.value })} />
          </Field>
          <details className="md:col-span-2 text-xs text-text-2">
            <summary className="cursor-pointer">OAuth 高级设置</summary>
            <div className="mt-3 grid gap-3 md:grid-cols-2">
              <Field label="预注册 Client ID（可选）"><input className="field" value={server.oauth_client_id ?? ''} onChange={(e) => onChange({ oauth_client_id: e.target.value })} /></Field>
              <Field label="Client Secret（可选，机密客户端）"><input className="field" type="password" value={server.oauth_client_secret ?? ''} onChange={(e) => onChange({ oauth_client_secret: e.target.value })} /></Field>
              <Field label="客户端元数据 URL（可选）"><input className="field" value={server.oauth_client_metadata_url ?? ''} onChange={(e) => onChange({ oauth_client_metadata_url: e.target.value })} /></Field>
              <Field label="本机回调端口（0 为自动）"><input className="field" type="number" min="0" max="65535" value={server.oauth_callback_port ?? 0} onChange={(e) => onChange({ oauth_callback_port: Number(e.target.value) })} /></Field>
              <p>预注册回调地址：http://127.0.0.1:端口/oauth/callback。浏览器需与 NekoCode 在同一台机器。</p>
            </div>
          </details>
          <div className="md:col-span-2 space-y-2 text-xs">
            <p role="status">{!server.enabled ? '未启用' : (statusLabels[health?.status ?? ''] ?? '保存后连接')}</p>
            <div className="flex gap-2">
              <button className="secondary-button" type="button" disabled={authBusy || !server.enabled || health?.status === 'authorizing'} onClick={() => void authorize('login')}>保存并授权</button>
              {health?.status === 'authorizing' && <button className="secondary-button" type="button" disabled={authBusy} onClick={() => void authorize('cancel')}>取消授权</button>}
              <button className="secondary-button" type="button" disabled={authBusy || !health} onClick={() => void authorize('logout')}>退出登录</button>
            </div>
            {health?.authUrl && <div><span>浏览器未打开时，复制链接到本机浏览器：</span><input aria-label="授权链接" className="field" readOnly value={health.authUrl} onFocus={(e) => e.target.select()} /></div>}
            {(authError || health?.error) && <p role="alert" className="text-danger">{authError || health?.error}</p>}
          </div>
        </> : <>
        <Field label="Command">
          <input className="field font-mono" value={server.command} onChange={(e) => onChange({ command: e.target.value })} />
        </Field>
        <Field label="Args（每行一个）">
          <textarea
            className="field min-h-[84px] resize-y py-2 font-mono"
            value={argsText}
            onChange={(e) => onChange({ args: splitLines(e.target.value) })}
          />
        </Field>
        <div className="md:col-span-2">
          <Field label="Env（每行 KEY=VALUE）">
            <textarea
              className="field min-h-[84px] resize-y py-2 font-mono"
              value={envText}
              onChange={(e) => onChange({ env: parseEnvLines(e.target.value) })}
            />
          </Field>
        </div>
        </>}
      </div>
    </div>
  )
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="block min-w-0">
      <span className="mb-1 block text-[11px] font-medium text-text-3">{label}</span>
      {children}
    </label>
  )
}

function ConfigShortcut({ title, detail, onClick }: { title: string; detail: string; onClick: () => void }) {
  return (
    <button
      type="button"
      className="rounded-md border border-border/50 bg-surface px-3 py-3 text-left transition-all hover:bg-surface-3 active:scale-[0.99]"
      onClick={onClick}
    >
      <span className="block text-xs font-semibold text-text">{title}</span>
      <span className="mt-1 block truncate text-[11px] text-text-3">{detail}</span>
    </button>
  )
}

function SectionTitle({ title, action, onAction }: { title: string; action: string; onAction: () => void }) {
  return (
    <div className="flex items-center gap-3">
      <h3 className="text-xs font-semibold text-text">{title}</h3>
      <span className="h-px flex-1 bg-border/60" />
      <button type="button" className="secondary-button" onClick={onAction}>{action}</button>
    </div>
  )
}

function StatusPill({ ok, text }: { ok: boolean; text: string }) {
  return (
    <span className={cn('inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-[11px]', ok ? 'bg-success/12 text-success' : 'bg-warning/12 text-warning')}>
      <span className={cn('h-1.5 w-1.5 rounded-full', ok ? 'bg-success' : 'bg-warning')} />
      {text}
    </span>
  )
}

function validateConfig(cfg: ConfigView | null): string {
  if (!cfg) return ''
  if (!cfg.models.length) return '至少需要一个文本模型'
  const names = new Set<string>()
  for (const [idx, model] of cfg.models.entries()) {
    const name = model.name.trim()
    if (!name) return `第 ${idx + 1} 个模型缺少名称`
    if (names.has(name)) return `模型名称重复：${name}`
    names.add(name)
    if (!model.provider.trim()) return `${name} 缺少 provider`
    if (!model.model.trim()) return `${name} 缺少模型 ID`
    if ((model.context_window ?? 0) < 0) return `${name} 的上下文窗口不能为负数`
    if (!model.profile) return `${name} 的模型能力正在解析`
  }
  if (!names.has(cfg.active)) return '当前模型不在模型列表中'
  if (cfg.flash_model && !names.has(cfg.flash_model)) return 'Flash 模型不在模型列表中'
  if (cfg.auto_compact_percent < 1 || cfg.auto_compact_percent > 99) return '自动压缩门限必须在 1% 到 99% 之间'
  const mcpNames = new Set<string>()
  for (const [rawName, srv] of Object.entries(cfg.mcp_servers ?? {})) {
    const name = rawName.trim()
    if (!name) return 'MCP 服务缺少名称'
    if (mcpNames.has(name)) return `MCP 服务名称重复：${name}`
    mcpNames.add(name)
    if (!srv.command.trim() && !srv.url?.trim()) return `${name} 缺少 command 或 MCP URL`
    if (srv.url?.trim()) {
      try { const url = new URL(srv.url); if (!['http:', 'https:'].includes(url.protocol)) return `${name} 的 URL 无效` }
      catch { return `${name} 的 URL 无效` }
    }
    for (const key of Object.keys(srv.env ?? {})) {
      if (!key.trim()) return `${name} 存在空 env key`
    }
  }
  return ''
}

function nextName(names: string[], base: string): string {
  const used = new Set(names)
  for (let i = 1; i < 100; i++) {
    const name = `${base}-${i}`
    if (!used.has(name)) return name
  }
  return `${base}-${Date.now()}`
}

function trimModel(model: ModelConfig): ModelConfig {
  return {
    name: model.name.trim(),
    provider: model.provider.trim(),
    api_key: model.api_key.trim(),
    model: model.model.trim(),
    base_url: model.base_url?.trim(),
    protocol: model.protocol || 'openai',
    reasoning_effort: model.reasoning_effort || '',
    context_window: model.context_window && model.context_window > 0 ? model.context_window : undefined,
  }
}

function formatNumber(value: number): string {
  return value > 0 ? new Intl.NumberFormat('en-US').format(value) : '—'
}

function contextWindowSource(source?: string): string {
  switch (source) {
    case 'override': return '自定义'
    case 'model': return '模型内置'
    case 'default': return '系统默认'
    default: return '解析中'
  }
}

function trimImageModel(model: ImageGenConfig): ImageGenConfig {
  return {
    name: model.name.trim(),
    provider: model.provider.trim(),
    api_key: model.api_key.trim(),
    secret_key: model.secret_key.trim(),
    base_url: model.base_url?.trim(),
    model: model.model?.trim(),
  }
}

function trimMcpServers(servers: Record<string, MCPServerConfig>): Record<string, MCPServerConfig> {
  const out: Record<string, MCPServerConfig> = {}
  for (const [name, server] of Object.entries(servers)) {
    const trimmedName = name.trim()
    if (!trimmedName) continue
    out[trimmedName] = {
      url: server.url?.trim(),
      oauth_client_id: server.oauth_client_id?.trim(),
      oauth_client_secret: server.oauth_client_secret?.trim(),
      oauth_client_metadata_url: server.oauth_client_metadata_url?.trim(),
      oauth_callback_port: server.oauth_callback_port,
      command: server.command.trim(),
      args: (server.args ?? []).map((arg) => arg.trim()).filter(Boolean),
      env: trimEnv(server.env ?? {}),
      enabled: server.enabled,
    }
  }
  return out
}

function splitLines(value: string): string[] {
  return value
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
}

function parseEnvLines(value: string): Record<string, string> {
  const env: Record<string, string> = {}
  for (const line of value.split('\n')) {
    const trimmed = line.trim()
    if (!trimmed) continue
    const idx = trimmed.indexOf('=')
    if (idx < 0) {
      env[trimmed] = ''
    } else {
      env[trimmed.slice(0, idx).trim()] = trimmed.slice(idx + 1).trim()
    }
  }
  return env
}

function trimEnv(env: Record<string, string>): Record<string, string> | undefined {
  const out: Record<string, string> = {}
  for (const [key, value] of Object.entries(env)) {
    const trimmedKey = key.trim()
    if (trimmedKey) out[trimmedKey] = value.trim()
  }
  return Object.keys(out).length ? out : undefined
}

function GearIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden>
      <path d="M12 15.5a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7Z" />
      <path d="M19.4 15a1.8 1.8 0 0 0 .36 1.98l.04.04a2 2 0 0 1-2.82 2.82l-.04-.04a1.8 1.8 0 0 0-1.98-.36 1.8 1.8 0 0 0-1.1 1.66V21a2 2 0 0 1-4 0v-.06A1.8 1.8 0 0 0 8.8 19.3a1.8 1.8 0 0 0-1.98.36l-.04.04a2 2 0 1 1-2.82-2.82l.04-.04A1.8 1.8 0 0 0 4.36 15a1.8 1.8 0 0 0-1.66-1.1H2.6a2 2 0 0 1 0-4h.06A1.8 1.8 0 0 0 4.3 8.8a1.8 1.8 0 0 0-.36-1.98l-.04-.04a2 2 0 1 1 2.82-2.82l.04.04A1.8 1.8 0 0 0 8.8 4.36a1.8 1.8 0 0 0 1.1-1.66V2.6a2 2 0 0 1 4 0v.06a1.8 1.8 0 0 0 1.1 1.64 1.8 1.8 0 0 0 1.98-.36l.04-.04a2 2 0 1 1 2.82 2.82l-.04.04a1.8 1.8 0 0 0-.36 1.98 1.8 1.8 0 0 0 1.66 1.1h.1a2 2 0 0 1 0 4h-.06A1.8 1.8 0 0 0 19.4 15Z" />
    </svg>
  )
}

function CloseIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" aria-hidden>
      <path d="M18 6 6 18" />
      <path d="m6 6 12 12" />
    </svg>
  )
}
