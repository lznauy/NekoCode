import { useEffect, useMemo, useState } from 'react'
import { cn } from '../lib/classnames'
import {
  isWailsEnvironment,
  safeRefreshSkillManagement,
  safeSetPluginEnabled,
  safeSkillManagementSnapshot,
} from '../lib/wails'
import type {
  MCPServerSnapshot,
  PluginSnapshot,
  SkillManagementSnapshot,
  SkillSnapshot,
  SkillSourceKind,
} from '../types/skills'

interface SkillPanelProps {
  open: boolean
  onClose: () => void
}

type Tab = 'skills' | 'plugins' | 'mcp'
type Filter = 'all' | 'loaded' | 'builtin' | 'local' | 'plugin'

export function SkillPanel({ open, onClose }: SkillPanelProps) {
  const [snapshot, setSnapshot] = useState<SkillManagementSnapshot | null>(null)
  const [tab, setTab] = useState<Tab>('skills')
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
  const mcpServers = snapshot?.mcp ?? []
  const loadedCount = skills.filter((skill) => skill.loaded).length
  const pluginSkillCount = skills.filter((skill) => skill.sourceKind === 'plugin').length
  const localSkillCount = skills.filter((skill) => skill.sourceKind === 'local').length
  const enabledPluginCount = plugins.filter((plugin) => plugin.enabled).length

  const filteredSkills = useMemo(() => {
    const q = query.trim().toLowerCase()
    return skills.filter((skill) => {
      const matchesQuery = !q || [skill.name, skill.description, skill.dir, skill.plugin]
        .filter(Boolean)
        .some((value) => value!.toLowerCase().includes(q))
      if (!matchesQuery) return false
      if (filter === 'loaded') return skill.loaded
      if (filter === 'builtin') return skill.sourceKind === 'builtin'
      if (filter === 'local') return skill.sourceKind === 'local'
      if (filter === 'plugin') return skill.sourceKind === 'plugin'
      return true
    })
  }, [filter, query, skills])

  const filteredPlugins = useMemo(() => {
    const q = query.trim().toLowerCase()
    return plugins.filter((plugin) => {
      if (!q) return true
      return [plugin.name, plugin.description, plugin.dir]
        .filter(Boolean)
        .some((value) => value!.toLowerCase().includes(q))
    })
  }, [query, plugins])

  const filteredMcp = useMemo(() => {
    const q = query.trim().toLowerCase()
    return mcpServers.filter((srv) => {
      if (!q) return true
      return [srv.name, srv.plugin, srv.command]
        .filter(Boolean)
        .some((value) => value!.toLowerCase().includes(q))
    })
  }, [query, mcpServers])

  if (!open) return null

  function applySnapshot(next: SkillManagementSnapshot | null) {
    if (!next) {
      setError('无法读取 skill 管理数据：Wails 接口没有返回数据')
      return
    }
    const normalized: SkillManagementSnapshot = {
      skills: next.skills ?? [],
      plugins: next.plugins ?? [],
      mcp: next.mcp ?? [],
    }
    setSnapshot(normalized)
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

  const tabCounts: Record<Tab, number> = {
    skills: skills.length,
    plugins: plugins.length,
    mcp: mcpServers.length,
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
              {skills.length ? `${skills.length} skills · ${plugins.length} plugins · ${mcpServers.length} MCP` : '读取可用资源...'}
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

        <nav className="flex gap-1 border-b border-border/60 px-5 py-2">
          {tabOptions.map((option) => (
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
              <span className={cn('rounded-sm px-1 text-[10px]', tab === option.value ? 'bg-black/15' : 'bg-surface')}>
                {tabCounts[option.value]}
              </span>
            </button>
          ))}
        </nav>

        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
          {loading && <div className="text-sm text-text-2">正在扫描注册表...</div>}
          {!loading && tab === 'skills' && (
            <SkillsView
              filteredSkills={filteredSkills}
              plugins={plugins}
              query={query}
              setQuery={setQuery}
              filter={filter}
              setFilter={setFilter}
              metrics={{ total: skills.length, loaded: loadedCount, plugin: pluginSkillCount, local: localSkillCount }}
            />
          )}
          {!loading && tab === 'plugins' && (
            <PluginsView
              plugins={filteredPlugins}
              query={query}
              setQuery={setQuery}
              mutating={mutating}
              onToggle={togglePlugin}
              enabledCount={enabledPluginCount}
            />
          )}
          {!loading && tab === 'mcp' && (
            <McpView servers={filteredMcp} query={query} setQuery={setQuery} />
          )}
        </div>

        <footer className="flex min-h-[48px] items-center border-t border-border/60 px-5">
          <div className="min-w-0 flex-1 text-xs">
            {error ? <span className="text-danger">{error}</span> : <span className="text-text-3">Skill 内容由 agent 按需加载；这里管理可见性和来源状态。</span>}
          </div>
        </footer>
      </aside>
    </div>
  )
}

const tabOptions: Array<{ value: Tab; label: string }> = [
  { value: 'skills', label: '技能' },
  { value: 'plugins', label: '插件' },
  { value: 'mcp', label: 'MCP' },
]

const filterOptions: Array<{ value: Filter; label: string }> = [
  { value: 'all', label: '全部' },
  { value: 'loaded', label: '已加载' },
  { value: 'builtin', label: '内置' },
  { value: 'local', label: '本地' },
  { value: 'plugin', label: '插件' },
]

function SkillsView(props: {
  filteredSkills: SkillSnapshot[]
  plugins: PluginSnapshot[]
  query: string
  setQuery: (v: string) => void
  filter: Filter
  setFilter: (v: Filter) => void
  metrics: { total: number; loaded: number; plugin: number; local: number }
}) {
  const { filteredSkills, plugins, query, setQuery, filter, setFilter, metrics } = props

  const enabledPlugins = plugins.filter((p) => p.enabled)
  const builtinSkills = filteredSkills.filter((s) => s.sourceKind === 'builtin')
  const localSkills = filteredSkills.filter((s) => s.sourceKind === 'local')
  const pluginSkillsByPlugin = useMemo(() => {
    const map = new Map<string, SkillSnapshot[]>()
    for (const s of filteredSkills) {
      if (s.sourceKind !== 'plugin' || !s.plugin) continue
      const list = map.get(s.plugin) ?? []
      list.push(s)
      map.set(s.plugin, list)
    }
    return map
  }, [filteredSkills])

  return (
    <div className="space-y-4">
      <section className="grid gap-2 md:grid-cols-4">
        <Metric label="全部技能" value={metrics.total} tone="primary" />
        <Metric label="本会话已加载" value={metrics.loaded} tone="success" />
        <Metric label="本地独立" value={metrics.local} tone="accent" />
        <Metric label="插件带来" value={metrics.plugin} tone="warning" />
      </section>

      <section className="rounded-md border border-border/50 bg-surface">
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

        <div className="max-h-[58vh] overflow-y-auto">
          {builtinSkills.length > 0 && <SkillGroup title="内置" skills={builtinSkills} />}
          {localSkills.length > 0 && <SkillGroup title="本地技能（独立安装）" skills={localSkills} />}
          {enabledPlugins.map((plugin) => {
            const groupSkills = pluginSkillsByPlugin.get(plugin.name) ?? []
            if (groupSkills.length === 0) return null
            return (
              <SkillGroup
                key={plugin.name}
                title={`插件 · ${plugin.name}`}
                subtitle={plugin.version ? `v${plugin.version}` : undefined}
                skills={groupSkills}
              />
            )
          })}
          {filteredSkills.length === 0 && (
            <div className="px-4 py-10 text-center text-xs text-text-3">没有匹配的 skill</div>
          )}
        </div>
      </section>
    </div>
  )
}

function SkillGroup(props: { title: string; subtitle?: string; skills: SkillSnapshot[] }) {
  const { title, subtitle, skills } = props
  return (
    <div className="border-b border-border/40 last:border-b-0">
      <div className="sticky top-0 z-10 flex items-center gap-2 bg-surface-2/95 px-3 py-1.5 backdrop-blur">
        <span className="text-[11px] font-semibold text-text-2">{title}</span>
        {subtitle && <span className="text-[10px] text-text-3">{subtitle}</span>}
        <span className="h-px flex-1 bg-border/40" />
        <span className="text-[10px] text-text-3">{skills.length}</span>
      </div>
      {skills.map((skill) => (
        <SkillRow key={skill.name} skill={skill} />
      ))}
    </div>
  )
}

function SkillRow({ skill }: { skill: SkillSnapshot }) {
  const [expanded, setExpanded] = useState(false)
  const files = skill.files ?? []
  return (
    <div className="border-b border-border/30 last:border-b-0">
      <button
        type="button"
        className="grid w-full grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-2 px-3 py-2 text-left transition-colors hover:bg-surface-3/70 active:scale-[0.997]"
        onClick={() => setExpanded((v) => !v)}
        aria-expanded={expanded}
      >
        <span className="shrink-0 text-text-3">
          <ChevronIcon open={expanded} />
        </span>
        <span className="min-w-0">
          <span className="flex min-w-0 items-center gap-2">
            <span className="truncate text-xs font-semibold text-text">{skill.name}</span>
            {skill.loaded && <span className="rounded-sm bg-success/12 px-1.5 py-0.5 text-[10px] text-success">已加载</span>}
          </span>
          <span className="mt-1 line-clamp-1 block text-[11px] text-text-3">{skill.description || '暂无描述'}</span>
        </span>
        <SourcePill skill={skill} />
      </button>
      {expanded && (
        <div className="space-y-2 bg-surface-3/40 px-3 py-2.5 text-[11px]">
          <div>
            <span className="font-medium text-text-3">说明：</span>
            <span className="break-words text-text-2">{skill.description || '暂无描述'}</span>
          </div>
          <div>
            <span className="font-medium text-text-3">路径：</span>
            <span className="break-all font-mono text-text-2">{skill.dir || '内置 skill'}</span>
          </div>
          <div>
            <span className="font-medium text-text-3">附带文件：</span>
            {files.length > 0 ? (
              <div className="mt-1 max-h-28 overflow-y-auto rounded-md bg-surface-3/70 px-2 py-1.5">
                {files.map((file) => (
                  <div key={file} className="truncate font-mono text-text-2">{file}</div>
                ))}
              </div>
            ) : (
              <span className="text-text-3">无额外文件</span>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

function PluginsView(props: {
  plugins: PluginSnapshot[]
  query: string
  setQuery: (v: string) => void
  mutating: string
  onToggle: (plugin: PluginSnapshot) => void
  enabledCount: number
}) {
  const { plugins, query, setQuery, mutating, onToggle, enabledCount } = props
  return (
    <div className="space-y-4">
      <section className="grid gap-2 md:grid-cols-3">
        <Metric label="全部插件" value={plugins.length} tone="primary" />
        <Metric label="已启用" value={enabledCount} tone="success" />
        <Metric label="已停用" value={plugins.length - enabledCount} tone="warning" />
      </section>
      <section className="rounded-md border border-border/50 bg-surface">
        <div className="flex flex-col gap-2 border-b border-border/50 p-3 md:flex-row md:items-center">
          <div className="relative min-w-0 flex-1">
            <SearchIcon />
            <input
              className="field h-8 pl-8"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="搜索插件名称、描述或路径"
            />
          </div>
        </div>
        <div className="divide-y divide-border/40">
          {plugins.map((plugin) => (
            <PluginRow key={plugin.name} plugin={plugin} mutating={mutating} onToggle={onToggle} />
          ))}
          {plugins.length === 0 && <div className="px-4 py-8 text-center text-xs text-text-3">没有匹配的插件</div>}
        </div>
      </section>
    </div>
  )
}

function PluginRow(props: { plugin: PluginSnapshot; mutating: string; onToggle: (plugin: PluginSnapshot) => void }) {
  const { plugin: p, mutating, onToggle } = props
  const [expanded, setExpanded] = useState(false)
  const badges = bundleBadges(p)
  const hasBundle = badges.length > 0
  return (
    <div className="px-4 py-3">
      <div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_auto] md:items-center">
        <div className="min-w-0">
          <div className="flex min-w-0 items-center gap-2">
            <button
              type="button"
              className={cn(
                'shrink-0 transition-transform',
                hasBundle ? 'text-text-3 hover:text-text' : 'cursor-default opacity-30',
              )}
              disabled={!hasBundle}
              onClick={() => setExpanded((v) => !v)}
              aria-label={expanded ? '折叠捆绑明细' : '展开捆绑明细'}
            >
              <ChevronIcon open={expanded} />
            </button>
            <span className="truncate text-xs font-semibold text-text">{p.name}</span>
            {p.version && <span className="text-[10px] text-text-3">v{p.version}</span>}
            {!p.enabled && <span className="rounded-sm bg-surface-3 px-1.5 py-0.5 text-[10px] text-text-3">已停用</span>}
          </div>
          <p className="mt-1 line-clamp-1 text-[11px] text-text-3">{p.description || p.dir || '暂无插件描述'}</p>
          {hasBundle && (
            <div className="mt-1.5 flex flex-wrap gap-1">
              {badges.map((b) => (
                <span key={b.label} className={cn('rounded-sm px-1.5 py-0.5 text-[10px]', b.tone)}>{b.label}</span>
              ))}
            </div>
          )}
        </div>
        <button
          type="button"
          className={cn(
            'inline-flex h-8 w-[86px] items-center justify-center rounded-md text-xs font-semibold transition-all active:scale-95 disabled:opacity-50',
            p.enabled ? 'bg-success/15 text-success hover:bg-success/20' : 'bg-surface-3 text-text-2 hover:text-text',
          )}
          disabled={mutating === p.name}
          onClick={() => onToggle(p)}
        >
          {mutating === p.name ? '处理中' : p.enabled ? '已启用' : '已停用'}
        </button>
      </div>
      {expanded && hasBundle && (
        <div className="mt-3 grid gap-2 rounded-md bg-surface-3/60 px-3 py-2 text-[11px] text-text-2 md:grid-cols-2">
          <BundleList title="技能" items={p.skillNames} empty="无" />
          <BundleList title="MCP 服务器" items={p.mcpServers} empty="无" />
          <BundleList title="Agents" items={p.agents} empty="无" />
          <BundleList title="命令" items={p.commands} empty="无" />
          <div className="md:col-span-2">
            <span className="font-medium text-text-3">Hooks：</span>
            <span>{p.hasHooks ? '已声明' : '无'}</span>
          </div>
        </div>
      )}
    </div>
  )
}

function BundleList(props: { title: string; items?: string[]; empty: string }) {
  const { title, items, empty } = props
  const list = items ?? []
  return (
    <div className="min-w-0">
      <span className="font-medium text-text-3">{title}：</span>
      {list.length > 0 ? (
        <span className="break-words text-text-2">{list.join('、')}</span>
      ) : (
        <span className="text-text-3">{empty}</span>
      )}
    </div>
  )
}

function bundleBadges(plugin: PluginSnapshot): Array<{ label: string; tone: string }> {
  const out: Array<{ label: string; tone: string }> = []
  const skills = plugin.skillNames?.length ?? 0
  const mcp = plugin.mcpServers?.length ?? 0
  const agents = plugin.agents?.length ?? 0
  const commands = plugin.commands?.length ?? 0
  if (skills > 0) out.push({ label: `${skills} skills`, tone: 'bg-primary/15 text-primary' })
  if (mcp > 0) out.push({ label: `${mcp} MCP`, tone: 'bg-accent/15 text-accent' })
  if (agents > 0) out.push({ label: `${agents} agents`, tone: 'bg-success/15 text-success' })
  if (commands > 0) out.push({ label: `${commands} cmds`, tone: 'bg-warning/15 text-warning' })
  if (plugin.hasHooks) out.push({ label: 'hooks', tone: 'bg-surface-3 text-text-2' })
  return out
}

function McpView(props: { servers: MCPServerSnapshot[]; query: string; setQuery: (v: string) => void }) {
  const { servers, query, setQuery } = props
  const enabled = servers.filter((s) => s.pluginEnabled).length
  return (
    <div className="space-y-4">
      <section className="grid gap-2 md:grid-cols-3">
        <Metric label="全部 MCP" value={servers.length} tone="primary" />
        <Metric label="来源插件启用" value={enabled} tone="success" />
        <Metric label="来源插件停用" value={servers.length - enabled} tone="warning" />
      </section>
      <section className="rounded-md border border-border/50 bg-surface">
        <div className="flex flex-col gap-2 border-b border-border/50 p-3 md:flex-row md:items-center">
          <div className="relative min-w-0 flex-1">
            <SearchIcon />
            <input
              className="field h-8 pl-8"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="搜索 server 名称、来源插件或命令"
            />
          </div>
        </div>
        <div className="divide-y divide-border/40">
          {servers.map((srv) => (
            <McpRow key={`${srv.plugin}/${srv.name}`} server={srv} />
          ))}
          {servers.length === 0 && <div className="px-4 py-8 text-center text-xs text-text-3">没有匹配的 MCP server</div>}
        </div>
      </section>
    </div>
  )
}

function McpRow({ server }: { server: MCPServerSnapshot }) {
  const cmd = [server.command, ...(server.args ?? [])].join(' ')
  return (
    <div className="grid gap-2 px-4 py-3 md:grid-cols-[minmax(0,1fr)_auto] md:items-center">
      <div className="min-w-0">
        <div className="flex min-w-0 items-center gap-2">
          <span className="truncate text-xs font-semibold text-text">{server.name}</span>
          <span className="shrink-0 rounded-sm bg-accent/12 px-1.5 py-0.5 text-[10px] text-accent">{server.plugin}</span>
          {server.dangerLevel && (
            <span className={cn('shrink-0 rounded-sm px-1.5 py-0.5 text-[10px]', dangerTone(server.dangerLevel))}>
              {server.dangerLevel}
            </span>
          )}
          {!server.pluginEnabled && <span className="shrink-0 rounded-sm bg-surface-3 px-1.5 py-0.5 text-[10px] text-text-3">插件已停用</span>}
        </div>
        <p className="mt-1 line-clamp-1 font-mono text-[11px] text-text-3">{cmd || '未配置命令'}</p>
      </div>
    </div>
  )
}

function dangerTone(level: string): string {
  const l = level.toLowerCase()
  if (l === 'high' || l === 'danger' || l === 'dangerous') return 'bg-danger/15 text-danger'
  if (l === 'medium' || l === 'moderate') return 'bg-warning/15 text-warning'
  return 'bg-surface-3 text-text-2'
}

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

function SourcePill({ skill }: { skill: SkillSnapshot }) {
  const tone = sourceKindTone(skill.sourceKind)
  return (
    <span className={cn('shrink-0 rounded-sm px-1.5 py-0.5 text-[10px]', tone)}>
      {skill.sourceKind === 'plugin' && skill.plugin ? skill.plugin : skill.source}
    </span>
  )
}

function sourceKindTone(kind: SkillSourceKind): string {
  if (kind === 'builtin') return 'bg-surface-3 text-text-2'
  if (kind === 'local') return 'bg-success/12 text-success'
  return 'bg-primary/12 text-primary'
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

function ChevronIcon({ open }: { open: boolean }) {
  return (
    <svg
      className={cn('transition-transform', open && 'rotate-90')}
      width="12"
      height="12"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2.4"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
    >
      <path d="m9 6 6 6-6 6" />
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
