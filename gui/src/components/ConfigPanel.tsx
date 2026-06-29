import { useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { cn } from '../lib/classnames'
import { isWailsEnvironment, safeGetConfig, safeSaveConfig } from '../lib/wails'
import type { ConfigSnapshot, ImageGenConfig, MCPServerConfig, ModelConfig } from '../types/config'

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
})

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
  dangerLevel: 'safe',
  enabled: true,
})

export type ConfigTab = 'overview' | 'models' | 'mcp'

const configTabs: Array<{ value: ConfigTab; label: string }> = [
  { value: 'overview', label: '概览' },
  { value: 'models', label: '模型' },
  { value: 'mcp', label: 'MCP 服务' },
]

export function ConfigPanel({ open, onClose, onSaved, initialTab = 'overview' }: ConfigPanelProps) {
  const [cfg, setCfg] = useState<ConfigSnapshot | null>(null)
  const [tab, setTab] = useState<ConfigTab>(initialTab)
  const [selectedMcp, setSelectedMcp] = useState('')
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [saved, setSaved] = useState(false)

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
          models: next.models ?? [],
          image_gen_models: next.image_gen_models ?? [],
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

  if (!open) return null

  const update = (patch: Partial<ConfigSnapshot>) => {
    setSaved(false)
    setCfg((prev) => (prev ? { ...prev, ...patch } : prev))
  }

  const updateModel = (idx: number, patch: Partial<ModelConfig>) => {
    setSaved(false)
    setCfg((prev) => {
      if (!prev) return prev
      const models = prev.models.map((m, i) => (i === idx ? { ...m, ...patch } : m))
      const next: ConfigSnapshot = { ...prev, models }
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
      return { ...prev, models: [...prev.models, emptyModel(name)], active: prev.active || name }
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
      return { ...prev, image_gen_models: [...(prev.image_gen_models ?? []), emptyImageModel(nextName(names, 'image'))] }
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
        models: cfg.models.map(trimModel),
        image_gen_models: (cfg.image_gen_models ?? []).map(trimImageModel),
        mcp_servers: trimMcpServers(cfg.mcp_servers ?? {}),
      })
      if (!savedCfg) {
        setError('保存失败：Wails 配置接口没有返回数据')
        return
      }
      setCfg(savedCfg)
      setSaved(true)
      onSaved()
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
          <button className="icon-button" type="button" title="关闭" aria-label="关闭配置" onClick={onClose}>
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

              <section className="grid gap-3 rounded-md border border-border/50 bg-surface px-4 py-3 md:grid-cols-3">
                <Field label="当前模型">
                  <select className="field" value={cfg.active} onChange={(e) => update({ active: e.target.value })}>
                    {cfg.models.map((m) => (
                      <option key={m.name} value={m.name}>{m.name}</option>
                    ))}
                  </select>
                </Field>
                <Field label="Flash 模型">
                  <select className="field" value={cfg.flash_model ?? ''} onChange={(e) => update({ flash_model: e.target.value })}>
                    <option value="">跟随当前模型</option>
                    {cfg.models.map((m) => (
                      <option key={m.name} value={m.name}>{m.name}</option>
                    ))}
                  </select>
                </Field>
                <Field label="上下文窗口">
                  <input
                    className="field"
                    inputMode="numeric"
                    value={String(cfg.context_window || '')}
                    onChange={(e) => update({ context_window: Number(e.target.value) || 0 })}
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
                      key={`${model.name}-${idx}`}
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
                      key={`${model.name}-${idx}`}
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
          <select className="field" value={model.protocol || 'openai'} onChange={(e) => onChange({ protocol: e.target.value as ModelConfig['protocol'] })}>
            <option value="openai">openai</option>
            <option value="anthropic">anthropic</option>
          </select>
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

function McpServerCard({
  name,
  server,
  onRename,
  onChange,
  onRemove,
}: {
  name: string
  server: MCPServerConfig
  onRename: (name: string) => void
  onChange: (patch: Partial<MCPServerConfig>) => void
  onRemove: () => void
}) {
  const [draftName, setDraftName] = useState(name)
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
        <Field label="危险等级">
          <select
            className="field"
            value={server.dangerLevel || 'write'}
            onChange={(e) => onChange({ dangerLevel: e.target.value as MCPServerConfig['dangerLevel'] })}
          >
            <option value="safe">safe</option>
            <option value="write">write</option>
            <option value="danger">danger</option>
            <option value="forbidden">forbidden</option>
          </select>
        </Field>
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

function validateConfig(cfg: ConfigSnapshot | null): string {
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
  }
  if (!names.has(cfg.active)) return '当前模型不在模型列表中'
  if (cfg.flash_model && !names.has(cfg.flash_model)) return 'Flash 模型不在模型列表中'
  if (!cfg.context_window || cfg.context_window <= 0) return '上下文窗口必须大于 0'
  const mcpNames = new Set<string>()
  for (const [rawName, srv] of Object.entries(cfg.mcp_servers ?? {})) {
    const name = rawName.trim()
    if (!name) return 'MCP 服务缺少名称'
    if (mcpNames.has(name)) return `MCP 服务名称重复：${name}`
    mcpNames.add(name)
    if (!srv.command.trim()) return `${name} 缺少 command`
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
      command: server.command.trim(),
      args: (server.args ?? []).map((arg) => arg.trim()).filter(Boolean),
      env: trimEnv(server.env ?? {}),
      dangerLevel: server.dangerLevel || 'write',
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
