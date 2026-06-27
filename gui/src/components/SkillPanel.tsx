import { useEffect, useMemo, useState } from 'react'
import { cn } from '../lib/classnames'
import {
  isWailsEnvironment,
  safeRefreshSkillManagement,
  safeSetPluginEnabled,
  safeSkillManagementSnapshot,
} from '../lib/wails'
import type { PluginSnapshot, SkillManagementSnapshot, SkillSnapshot } from '../types/skills'

interface SkillPanelProps {
  open: boolean
  onClose: () => void
}

type Filter = 'all' | 'loaded' | 'local' | 'plugin' | 'builtin'

export function SkillPanel({ open, onClose }: SkillPanelProps) {
  const [snapshot, setSnapshot] = useState<SkillManagementSnapshot | null>(null)
  const [selected, setSelected] = useState('')
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState<Filter>('all')
  const [loading, setLoading] = useState(false)
  const [refreshing, setRefreshing] = useState(false)
  const [mutating, setMutating] = useState('')
  const [error, setError] = useState('')

  useEffect(() => {
    if (!open) return
    setLoading(true)
    setError('')
    if (!isWailsEnvironment()) {
      setSnapshot(null)
      setSelected('')
      setError('当前是浏览器预览环境，无法访问 Wails skill 管理接口；请通过 Wails GUI 运行后再打开技能管理。')
      setLoading(false)
      return
    }
    safeSkillManagementSnapshot()
      .then((next) => applySnapshot(next))
      .catch((err: unknown) => setError(err instanceof Error ? err.message : String(err)))
      .finally(() => setLoading(false))
  }, [open])

  const skills = snapshot?.skills ?? []
  const plugins = snapshot?.plugins ?? []
  const loadedCount = skills.filter((skill) => skill.loaded).length
  const pluginSkillCount = skills.filter((skill) => skill.source === '插件').length
  const enabledPluginCount = plugins.filter((plugin) => plugin.enabled).length

  const filteredSkills = useMemo(() => {
    const q = query.trim().toLowerCase()
    return skills.filter((skill) => {
      const matchesQuery = !q || [skill.name, skill.description, skill.dir, skill.plugin]
        .filter(Boolean)
        .some((value) => value!.toLowerCase().includes(q))
      if (!matchesQuery) return false
      if (filter === 'loaded') return skill.loaded
      if (filter === 'local') return skill.source === '本地'
      if (filter === 'plugin') return skill.source === '插件'
      if (filter === 'builtin') return skill.source === '内置'
      return true
    })
  }, [filter, query, skills])

  const selectedSkill = skills.find((skill) => skill.name === selected) ?? filteredSkills[0] ?? null

  if (!open) return null

  function applySnapshot(next: SkillManagementSnapshot | null) {
    if (!next) {
      setError('无法读取 skill 管理数据：Wails 接口没有返回数据')
      return
    }
    const normalized = {
      skills: next.skills ?? [],
      plugins: next.plugins ?? [],
    }
    setSnapshot(normalized)
    setSelected((prev) => (normalized.skills.some((skill) => skill.name === prev) ? prev : normalized.skills[0]?.name ?? ''))
  }

  async function refresh() {
    setRefreshing(true)
    setError('')
    try {
      applySnapshot(await safeRefreshSkillManagement())
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setRefreshing(false)
    }
  }

  async function togglePlugin(plugin: PluginSnapshot) {
    setMutating(plugin.name)
    setError('')
    try {
      applySnapshot(await safeSetPluginEnabled(plugin.name, !plugin.enabled))
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setMutating('')
    }
  }

  return (
    <div className="fixed inset-0 z-40 flex justify-end bg-black/35" onMouseDown={onClose}>
      <aside
        className="flex h-full w-full max-w-[980px] flex-col border-l border-border/70 bg-surface-2 surface-shadow animate-slide-in"
        onMouseDown={(e) => e.stopPropagation()}
      >
        <header className="flex min-h-[56px] items-center gap-3 border-b border-border/60 px-5">
          <div className="flex h-8 w-8 items-center justify-center rounded-md bg-primary/15 text-primary">
            <SparkIcon />
          </div>
          <div className="min-w-0 flex-1">
            <h2 className="text-sm font-semibold leading-tight text-text">技能管理</h2>
            <p className="mt-0.5 truncate text-[11px] text-text-3">
              {skills.length ? `${skills.length} 个 skills / ${plugins.length} 个 plugins` : '读取可用技能...'}
            </p>
          </div>
          <button className="secondary-button gap-1.5" type="button" disabled={refreshing || loading} onClick={refresh}>
            <RefreshIcon spinning={refreshing} />
            刷新
          </button>
          <button className="icon-button" type="button" title="关闭" aria-label="关闭技能管理" onClick={onClose}>
            <CloseIcon />
          </button>
        </header>

        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
          {loading && <div className="text-sm text-text-2">正在扫描 skill 注册表...</div>}
          {!loading && (
            <div className="space-y-4">
              <section className="grid gap-2 md:grid-cols-4">
                <Metric label="全部技能" value={skills.length} tone="primary" />
                <Metric label="本会话已加载" value={loadedCount} tone="success" />
                <Metric label="插件技能" value={pluginSkillCount} tone="accent" />
                <Metric label="启用插件" value={`${enabledPluginCount}/${plugins.length}`} tone="warning" />
              </section>

              <section className="grid gap-3 lg:grid-cols-[minmax(0,1fr)_320px]">
                <div className="min-w-0 rounded-md border border-border/50 bg-surface">
                  <div className="flex flex-col gap-2 border-b border-border/50 p-3 md:flex-row md:items-center">
                    <div className="relative min-w-0 flex-1">
                      <SearchIcon />
                      <input
                        className="field h-8 pl-8"
                        value={query}
                        onChange={(e) => setQuery(e.target.value)}
                        placeholder="搜索名称、描述、路径或插件"
                      />
                    </div>
                    <div className="flex flex-wrap gap-1">
                      {filterOptions.map((option) => (
                        <button
                          key={option.value}
                          type="button"
                          className={cn(
                            'h-8 rounded-md px-2.5 text-[11px] font-medium transition-all active:scale-95',
                            filter === option.value ? 'bg-primary text-black' : 'bg-surface-3 text-text-2 hover:text-text',
                          )}
                          onClick={() => setFilter(option.value)}
                        >
                          {option.label}
                        </button>
                      ))}
                    </div>
                  </div>

                  <div className="max-h-[52vh] overflow-y-auto">
                    {filteredSkills.map((skill) => (
                      <button
                        key={skill.name}
                        type="button"
                        className={cn(
                          'grid min-h-[58px] w-full grid-cols-[minmax(0,1fr)_auto] gap-3 border-b border-border/40 px-3 py-2 text-left transition-colors last:border-b-0 hover:bg-surface-3/70 active:scale-[0.997]',
                          selectedSkill?.name === skill.name && 'bg-primary/9',
                        )}
                        onClick={() => setSelected(skill.name)}
                      >
                        <span className="min-w-0">
                          <span className="flex min-w-0 items-center gap-2">
                            <span className="truncate text-xs font-semibold text-text">{skill.name}</span>
                            {skill.loaded && <span className="rounded-sm bg-success/12 px-1.5 py-0.5 text-[10px] text-success">已加载</span>}
                          </span>
                          <span className="mt-1 line-clamp-1 block text-[11px] text-text-3">
                            {skill.description || '暂无描述'}
                          </span>
                        </span>
                        <SourcePill skill={skill} />
                      </button>
                    ))}
                    {filteredSkills.length === 0 && (
                      <div className="px-4 py-10 text-center text-xs text-text-3">没有匹配的 skill</div>
                    )}
                  </div>
                </div>

                <SkillDetail skill={selectedSkill} />
              </section>

              <section className="rounded-md border border-border/50 bg-surface">
                <div className="flex items-center gap-3 border-b border-border/50 px-4 py-3">
                  <h3 className="text-xs font-semibold text-text">插件来源</h3>
                  <span className="h-px flex-1 bg-border/60" />
                  <span className="text-[11px] text-text-3">关闭插件后，其 skills 会从可用列表移除</span>
                </div>
                <div className="divide-y divide-border/40">
                  {plugins.map((plugin) => (
                    <div key={plugin.name} className="grid gap-3 px-4 py-3 md:grid-cols-[minmax(0,1fr)_auto] md:items-center">
                      <div className="min-w-0">
                        <div className="flex min-w-0 items-center gap-2">
                          <span className="truncate text-xs font-semibold text-text">{plugin.name}</span>
                          {plugin.version && <span className="text-[10px] text-text-3">v{plugin.version}</span>}
                        </div>
                        <p className="mt-1 line-clamp-1 text-[11px] text-text-3">{plugin.description || plugin.dir || '暂无插件描述'}</p>
                      </div>
                      <button
                        type="button"
                        className={cn(
                          'inline-flex h-8 w-[86px] items-center justify-center rounded-md text-xs font-semibold transition-all active:scale-95 disabled:opacity-50',
                          plugin.enabled ? 'bg-success/15 text-success hover:bg-success/20' : 'bg-surface-3 text-text-2 hover:text-text',
                        )}
                        disabled={mutating === plugin.name}
                        onClick={() => togglePlugin(plugin)}
                      >
                        {mutating === plugin.name ? '处理中' : plugin.enabled ? '已启用' : '已停用'}
                      </button>
                    </div>
                  ))}
                  {plugins.length === 0 && <div className="px-4 py-8 text-center text-xs text-text-3">暂无插件来源</div>}
                </div>
              </section>
            </div>
          )}
        </div>

        <footer className="flex min-h-[48px] items-center border-t border-border/60 px-5">
          <div className="min-w-0 flex-1 text-xs">
            {error ? <span className="text-danger">{error}</span> : <span className="text-text-3">Skill 文件内容仍由 agent 按需加载；这里管理可见性和来源状态。</span>}
          </div>
        </footer>
      </aside>
    </div>
  )
}

const filterOptions: Array<{ value: Filter; label: string }> = [
  { value: 'all', label: '全部' },
  { value: 'loaded', label: '已加载' },
  { value: 'builtin', label: '内置' },
  { value: 'local', label: '本地' },
  { value: 'plugin', label: '插件' },
]

function Metric({ label, value, tone }: { label: string; value: number | string; tone: 'primary' | 'success' | 'accent' | 'warning' }) {
  const toneClass = {
    primary: 'text-primary bg-primary/10',
    success: 'text-success bg-success/10',
    accent: 'text-accent bg-accent/10',
    warning: 'text-warning bg-warning/10',
  }[tone]
  return (
    <div className="rounded-md border border-border/50 bg-surface px-3 py-2.5">
      <div className={cn('mb-2 inline-flex rounded-sm px-1.5 py-0.5 text-[10px]', toneClass)}>{label}</div>
      <div className="text-xl font-semibold leading-none text-text">{value}</div>
    </div>
  )
}

function SkillDetail({ skill }: { skill: SkillSnapshot | null }) {
  if (!skill) {
    return (
      <div className="rounded-md border border-dashed border-border/70 bg-surface px-4 py-10 text-center text-xs text-text-3">
        选择一个 skill 查看详情
      </div>
    )
  }
  return (
    <aside className="rounded-md border border-border/50 bg-surface px-4 py-3">
      <div className="flex items-start gap-3">
        <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-primary/12 text-primary">
          <SparkIcon />
        </div>
        <div className="min-w-0 flex-1">
          <h3 className="truncate text-sm font-semibold text-text">{skill.name}</h3>
          <div className="mt-1 flex flex-wrap gap-1.5">
            <SourcePill skill={skill} />
            {skill.loaded && <span className="rounded-sm bg-success/12 px-1.5 py-0.5 text-[10px] text-success">本会话已加载</span>}
          </div>
        </div>
      </div>
      <DetailBlock title="说明" value={skill.description || '暂无描述'} />
      <DetailBlock title="路径" value={skill.dir || '内置 skill'} mono />
      <div className="mt-3">
        <div className="mb-1 text-[11px] font-medium text-text-3">附带文件</div>
        <div className="max-h-28 overflow-y-auto rounded-md bg-surface-3/70 px-2 py-1.5">
          {(skill.files ?? []).length > 0 ? (
            skill.files!.map((file) => <div key={file} className="truncate font-mono text-[11px] text-text-2">{file}</div>)
          ) : (
            <div className="text-[11px] text-text-3">无额外文件</div>
          )}
        </div>
      </div>
    </aside>
  )
}

function DetailBlock({ title, value, mono }: { title: string; value: string; mono?: boolean }) {
  return (
    <div className="mt-3">
      <div className="mb-1 text-[11px] font-medium text-text-3">{title}</div>
      <p className={cn('break-words text-xs text-text-2', mono && 'font-mono text-[11px]')}>{value}</p>
    </div>
  )
}

function SourcePill({ skill }: { skill: SkillSnapshot }) {
  return (
    <span className="shrink-0 rounded-sm bg-surface-3 px-1.5 py-0.5 text-[10px] text-text-2">
      {skill.plugin || skill.source}
    </span>
  )
}

function SearchIcon() {
  return (
    <svg className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-text-3" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" aria-hidden>
      <circle cx="11" cy="11" r="7" />
      <path d="m20 20-3.5-3.5" />
    </svg>
  )
}

function RefreshIcon({ spinning }: { spinning: boolean }) {
  return (
    <svg className={cn(spinning && 'animate-spin')} width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <path d="M20 12a8 8 0 1 1-2.3-5.7" />
      <path d="M20 4v6h-6" />
    </svg>
  )
}

function SparkIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.1" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <path d="M12 3v4" />
      <path d="M12 17v4" />
      <path d="M3 12h4" />
      <path d="M17 12h4" />
      <path d="M12 8.5 13.2 11l2.3 1-2.3 1L12 15.5 10.8 13l-2.3-1 2.3-1L12 8.5Z" />
    </svg>
  )
}

function CloseIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <path d="M18 6 6 18" />
      <path d="m6 6 12 12" />
    </svg>
  )
}
