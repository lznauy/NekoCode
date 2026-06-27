import { useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { cn } from '../lib/classnames'
import { isWailsEnvironment, safeGetConfig, safeSaveConfig } from '../lib/wails'
import type { ConfigSnapshot, ImageGenConfig, ModelConfig } from '../types/config'

interface ConfigPanelProps {
  open: boolean
  onClose: () => void
  onSaved: () => void
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

export function ConfigPanel({ open, onClose, onSaved }: ConfigPanelProps) {
  const [cfg, setCfg] = useState<ConfigSnapshot | null>(null)
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [saved, setSaved] = useState(false)

  useEffect(() => {
    if (!open) return
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
        })
      })
      .catch((err: unknown) => setError(err instanceof Error ? err.message : String(err)))
      .finally(() => setLoading(false))
  }, [open])

  const validation = useMemo(() => validateConfig(cfg), [cfg])

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

        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
          {loading && <div className="text-sm text-text-2">正在识别配置文件...</div>}
          {!loading && cfg && (
            <div className="space-y-4">
              <section className="rounded-md border border-border/50 bg-surface px-4 py-3">
                <div className="flex flex-wrap items-center gap-2">
                  <StatusPill ok={cfg.exists} text={cfg.exists ? '已识别配置文件' : '未找到配置文件，保存后创建'} />
                  <span className="text-[11px] text-text-3">{cfg.models.length} 个文本模型</span>
                  <span className="text-[11px] text-text-3">{cfg.image_gen_models?.length ?? 0} 个图片模型</span>
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

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="block min-w-0">
      <span className="mb-1 block text-[11px] font-medium text-text-3">{label}</span>
      {children}
    </label>
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
