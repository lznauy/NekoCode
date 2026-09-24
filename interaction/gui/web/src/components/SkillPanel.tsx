import { useEffect, useMemo, useState } from 'react'
import { cn } from '../lib/classnames'
import {
  isWailsEnvironment,
  safeRefreshSkillManagement,
  safeSetPluginEnabled,
  safeSkillManagementView,
} from '../lib/wails'
import type {
  MCPServerView,
  PluginView,
  SkillManagementView,
  SkillView,
  SkillSourceKind,
} from '../types/skills'

interface SkillPanelProps {
  open: boolean
  onClose: () => void
  onConfigureMcp?: () => void
}

type Tab = 'skills' | 'plugins' | 'mcp'
type Filter = 'all' | 'loaded' | 'builtin' | 'local' | 'plugin'
type PluginFilter = 'all' | 'enabled' | 'disabled'
type McpFilter = 'all' | 'enabled' | 'disabled' | 'config' | 'plugin'

export function SkillPanel({ open, onClose, onConfigureMcp }: SkillPanelProps) {
  const [view, setView] = useState<SkillManagementView | null>(null)
  const [tab, setTab] = useState<Tab>('skills')
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState<Filter>('all')
  const [pluginFilter, setPluginFilter] = useState<PluginFilter>('all')
  const [mcpFilter, setMcpFilter] = useState<McpFilter>('all')
  const [loading, setLoading] = useState(false)
  const [refreshing, setRefreshing] = useState(false)
  const [mutating, setMutating] = useState('')
  const [error, setError] = useState('')

  useEffect(() => {
    if (!open) return
    setLoading(true)
    setError('')
    if (!isWailsEnvironment()) {
      setView(null)
      setError('当前是浏览器预览环境，无法访问 Wails skill 管理接口；请通过 Wails GUI 运行后再打开技能管理。')
      setLoading(false)
      return
    }
    safeSkillManagementView()
      .then((next) => applyView(next))
      .catch((err: unknown) => setError(err instanceof Error ? err.message : String(err)))
      .finally(() => setLoading(false))
  }, [open])

  const skills = view?.skills ?? []
  const plugins = view?.plugins ?? []
  const mcpServers = view?.mcp ?? []
  const loadedCount = skills.filter((skill) => skill.loaded).length
  const pluginSkillCount = skills.filter((skill) => skill.sourceKind === 'plugin').length
  const localSkillCount = skills.filter((skill) => skill.sourceKind === 'local').length
  const enabledPluginCount = plugins.filter((plugin) => plugin.enabled).length
  const readyMcpCount = mcpServers.filter((srv) => srv.status === 'ready').length

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
      if (pluginFilter === 'enabled' && !plugin.enabled) return false
      if (pluginFilter === 'disabled' && plugin.enabled) return false
      if (!q) return true
      return [plugin.name, plugin.description, plugin.dir]
        .filter(Boolean)
        .some((value) => value!.toLowerCase().includes(q))
    })
  }, [pluginFilter, query, plugins])

  const filteredMcp = useMemo(() => {
    const q = query.trim().toLowerCase()
    return mcpServers.filter((srv) => {
      if (mcpFilter === 'enabled' && srv.status !== 'ready') return false
      if (mcpFilter === 'disabled' && srv.status !== 'disabled') return false
      if (mcpFilter === 'config' && srv.plugin !== '配置') return false
      if (mcpFilter === 'plugin' && srv.plugin === '配置') return false
      if (!q) return true
      return [srv.name, srv.plugin, srv.command]
        .filter(Boolean)
        .some((value) => value!.toLowerCase().includes(q))
    })
  }, [mcpFilter, query, mcpServers])

  if (!open) return null

  function applyView(next: SkillManagementView | null) {
    if (!next) {
      setError('无法读取 skill 管理数据：Wails 接口没有返回数据')
      return
    }
    const normalized: SkillManagementView = {
      skills: next.skills ?? [],
      plugins: next.plugins ?? [],
      mcp: next.mcp ?? [],
    }
    setView(normalized)
  }

  async function refresh() {
    setRefreshing(true)
    setError('')
    try {
      applyView(await safeRefreshSkillManagement())
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setRefreshing(false)
    }
  }

  async function togglePlugin(plugin: PluginView) {
    setMutating(plugin.name)
    setError('')
    try {
      applyView(await safeSetPluginEnabled(plugin.name, !plugin.enabled))
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
        <header className="flex min-h-[64px] items-center gap-3 border-b border-border/60 bg-surface/35 px-5">
          <div className="flex h-9 w-9 items-center justify-center rounded-md bg-primary/12 text-primary">
            <SparkIcon />
          </div>
          <div className="min-w-0 flex-1">
            <h2 className="text-sm font-semibold leading-tight text-text">技能管理</h2>
            <div className="mt-1 flex min-w-0 flex-wrap items-center gap-1.5">
              <HeaderStat label="技能" value={skills.length} />
              <HeaderStat label="已加载" value={loadedCount} />
              <HeaderStat label="插件" value={`${enabledPluginCount}/${plugins.length}`} />
              <HeaderStat label="MCP" value={`${readyMcpCount}/${mcpServers.length}`} />
            </div>
          </div>
          <button className="secondary-button tooltip-anchor gap-1.5" type="button" disabled={refreshing || loading} onClick={refresh} data-tooltip="重新扫描技能、插件和 MCP">
            <RefreshIcon spinning={refreshing} />
            刷新
          </button>
          <button className="icon-button tooltip-anchor" type="button" data-tooltip="关闭" data-tooltip-align="end" aria-label="关闭技能管理" onClick={onClose}>
            <CloseIcon />
          </button>
        </header>

        <div className="grid min-h-0 flex-1 grid-cols-[168px_minmax(0,1fr)] overflow-hidden">
          <nav className="border-r border-border/60 bg-surface px-3 py-4">
            <div className="mb-2 px-2 text-[10px] font-semibold uppercase text-text-3">分类</div>
            <div className="space-y-1">
              {tabOptions.map((option) => (
                <button
                  key={option.value}
                  type="button"
                  className={cn(
                    'flex h-10 w-full items-center justify-between rounded-md px-2.5 text-left text-xs transition-all active:scale-[0.98]',
                    tab === option.value ? 'bg-primary text-black' : 'text-text-2 hover:bg-surface-3 hover:text-text',
                  )}
                  onClick={() => setTab(option.value)}
                >
                  <span className="font-medium">{option.label}</span>
                  <span className={cn('min-w-6 rounded-sm px-1 text-center font-mono text-[10px] tabular-nums', tab === option.value ? 'bg-black/15' : 'bg-surface-3 text-text-3')}>
                    {tabCounts[option.value]}
                  </span>
                </button>
              ))}
            </div>
          </nav>

          <div className="min-h-0 overflow-hidden px-5 py-4">
            {loading && <div className="rounded-md border border-border/45 bg-surface px-4 py-3 text-sm text-text-2">正在扫描注册表...</div>}
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
                filter={pluginFilter}
                setFilter={setPluginFilter}
                totalCount={plugins.length}
                enabledCount={enabledPluginCount}
              />
            )}
            {!loading && tab === 'mcp' && (
              <McpView
                servers={filteredMcp}
                allServers={mcpServers}
                query={query}
                setQuery={setQuery}
                filter={mcpFilter}
                setFilter={setMcpFilter}
                onConfigure={onConfigureMcp}
              />
            )}
          </div>
        </div>

        <footer className="flex min-h-[42px] items-center border-t border-border/60 bg-surface/35 px-5">
          <div className="min-w-0 flex-1 text-xs">
            {error ? <span className="text-danger">{error}</span> : <span className="text-text-3">管理技能、插件和 MCP 来源。</span>}
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
  filteredSkills: SkillView[]
  plugins: PluginView[]
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
    const map = new Map<string, SkillView[]>()
    for (const s of filteredSkills) {
      if (s.sourceKind !== 'plugin' || !s.plugin) continue
      const list = map.get(s.plugin) ?? []
      list.push(s)
      map.set(s.plugin, list)
    }
    return map
  }, [filteredSkills])

  return (
    <section className="overflow-hidden rounded-md border border-border/50 bg-surface">
      <div className="border-b border-border/45 px-4 py-3">
        <div className="mb-3 flex items-center justify-between gap-3">
          <div>
            <h3 className="text-sm font-semibold text-text">技能</h3>
            <p className="mt-0.5 text-[11px] text-text-3">
              共 {metrics.total} 个，已加载 {metrics.loaded} 个，本地 {metrics.local} 个，插件 {metrics.plugin} 个
            </p>
          </div>
          <span className="font-mono text-[11px] tabular-nums text-text-3">{filteredSkills.length}/{metrics.total}</span>
        </div>
        <div className="flex flex-col gap-2 lg:flex-row lg:items-center">
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
      </div>
      <ResultStrip shown={filteredSkills.length} total={metrics.total} query={query} extra={`${metrics.loaded} 已加载`} onClear={() => setQuery('')} />
      <div className="max-h-[calc(100vh-250px)] min-h-[360px] overflow-y-auto scrollbar-gutter-stable">
        {builtinSkills.length > 0 && <SkillGroup title="内置" skills={builtinSkills} />}
        {localSkills.length > 0 && <SkillGroup title="本地技能" skills={localSkills} />}
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
        {filteredSkills.length === 0 && <EmptyRows title="没有匹配的 skill" detail="换个关键词或清除搜索条件。" onClear={() => setQuery('')} />}
      </div>
    </section>
  )
}

function SkillGroup(props: { title: string; subtitle?: string; skills: SkillView[] }) {
  const { title, subtitle, skills } = props
  return (
    <div className="border-b border-border/40 last:border-b-0">
      <div className="sticky top-0 z-10 flex items-center gap-2 border-b border-border/25 bg-surface-2/95 px-3 py-1.5 backdrop-blur">
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

function SkillRow({ skill }: { skill: SkillView }) {
  const [expanded, setExpanded] = useState(false)
  const files = skill.files ?? []
  return (
    <div className={cn('border-b border-border/30 last:border-b-0', expanded && 'bg-surface-3/30')}>
      <button
        type="button"
        className="grid w-full grid-cols-[auto_minmax(120px,1fr)_auto] items-center gap-2 px-3 py-2 text-left transition-colors hover:bg-surface-3/60 active:scale-[0.997]"
        onClick={() => setExpanded((v) => !v)}
        aria-expanded={expanded}
      >
        <span className="shrink-0 text-text-3">
          <ChevronIcon open={expanded} />
        </span>
        <span className="min-w-0">
          <span className="flex min-w-0 items-center gap-2">
            <span className="truncate text-sm font-medium tracking-wide text-text">{skill.name}</span>
            {skill.loaded && <span className="rounded-sm bg-success/12 px-1.5 py-0.5 text-[10px] text-success">已加载</span>}
          </span>
        </span>
        <span className="flex shrink-0 items-center gap-1.5">
          <SourcePill skill={skill} />
          {files.length > 0 && <span className="rounded-sm bg-surface-3 px-1.5 py-0.5 text-[10px] text-text-3">{files.length} 个文件</span>}
        </span>
      </button>
      {expanded && (
        <div className="ml-8 mr-3 mb-3 grid gap-2 rounded-md border border-border/35 bg-surface/80 px-3 py-2.5 text-[11px] md:grid-cols-[minmax(0,1fr)_220px]">
          <div className="min-w-0 space-y-2">
            <KeyValue label="说明" value={skill.description || '暂无描述'} />
            <KeyValue label="路径" value={skill.dir || '内置 skill'} mono />
          </div>
          <div className="min-w-0">
            <div className="mb-1 font-medium text-text-3">附带文件</div>
            {files.length > 0 ? (
              <div className="max-h-28 overflow-y-auto rounded-md bg-surface-3/70 px-2 py-1.5">
                {files.map((file) => (
                  <div key={file} className="truncate font-mono text-text-2">{file}</div>
                ))}
              </div>
            ) : (
              <div className="rounded-md bg-surface-3/45 px-2 py-1.5 text-text-3">无额外文件</div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

function PluginsView(props: {
  plugins: PluginView[]
  query: string
  setQuery: (v: string) => void
  mutating: string
  onToggle: (plugin: PluginView) => void
  filter: PluginFilter
  setFilter: (v: PluginFilter) => void
  totalCount: number
  enabledCount: number
}) {
  const { plugins, query, setQuery, mutating, onToggle, filter, setFilter, totalCount, enabledCount } = props
  return (
    <section className="overflow-hidden rounded-md border border-border/50 bg-surface">
      <div className="border-b border-border/45 px-4 py-3">
        <div className="mb-3 flex items-center justify-between gap-3">
          <div>
            <h3 className="text-sm font-semibold text-text">插件</h3>
            <p className="mt-0.5 text-[11px] text-text-3">共 {totalCount} 个，启用 {enabledCount} 个，停用 {totalCount - enabledCount} 个</p>
          </div>
          <span className="font-mono text-[11px] tabular-nums text-text-3">{plugins.length}/{totalCount}</span>
        </div>
        <div className="flex flex-col gap-2 lg:flex-row lg:items-center">
          <div className="relative min-w-0 flex-1">
            <SearchIcon />
            <input
              className="field h-8 pl-8"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="搜索插件名称、描述或路径"
            />
          </div>
          <div className="shrink-0">
            <SegmentedFilter
              value={filter}
              options={[
                { value: 'all', label: '全部' },
                { value: 'enabled', label: '启用' },
                { value: 'disabled', label: '停用' },
              ]}
              onChange={(value) => setFilter(value as PluginFilter)}
            />
          </div>
        </div>
      </div>
      <ResultStrip shown={plugins.length} total={totalCount} query={query} extra={`${enabledCount} 已启用`} onClear={() => setQuery('')} />
      <div className="max-h-[calc(100vh-250px)] min-h-[360px] divide-y divide-border/40 overflow-y-auto scrollbar-gutter-stable">
        {plugins.map((plugin) => (
          <PluginRow key={plugin.name} plugin={plugin} mutating={mutating} onToggle={onToggle} />
        ))}
        {plugins.length === 0 && <EmptyRows title="没有匹配的插件" detail="清除搜索或切换启用状态过滤。" onClear={() => setQuery('')} />}
      </div>
    </section>
  )
}

function PluginRow(props: { plugin: PluginView; mutating: string; onToggle: (plugin: PluginView) => void }) {
  const { plugin: p, mutating, onToggle } = props
  const [expanded, setExpanded] = useState(false)
  const badges = bundleBadges(p)
  const hasBundle = badges.length > 0
  return (
    <div className={cn('px-4 py-3 transition-colors hover:bg-surface-3/35', !p.enabled && 'opacity-75')}>
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
            <span className="truncate text-sm font-medium tracking-wide text-text">{p.name}</span>
            {p.version && <span className="text-[10px] text-text-3">v{p.version}</span>}
            {!p.enabled && <span className="rounded-sm bg-surface-3 px-1.5 py-0.5 text-[10px] text-text-3">已停用</span>}
          </div>
          <p className="mt-1 truncate font-mono text-[10.5px] text-text-3">{p.dir || p.description || '无路径信息'}</p>
          {p.agentError && <p className="mt-1 break-words text-[11px] text-danger">Agent 加载失败：{p.agentError}</p>}
          {hasBundle && (
            <div className="mt-1.5 flex flex-wrap gap-1">
              {badges.map((b) => (
                <span key={b.label} className={cn('rounded-sm px-1.5 py-0.5 text-[10px]', b.tone)}>{b.label}</span>
              ))}
            </div>
          )}
        </div>
        <PluginSwitch
          enabled={p.enabled}
          busy={mutating === p.name}
          onClick={() => onToggle(p)}
        />
      </div>
      {expanded && hasBundle && (
        <div className="mt-3 grid gap-2 rounded-md bg-surface-3/60 px-3 py-2 text-[11px] text-text-2 md:grid-cols-2">
          <BundleList title="技能" items={p.skillNames} empty="无" />
          <BundleList title="MCP 服务器" items={p.mcpServers} empty="无" />
          <BundleList title="Agents" items={p.agents} empty="无" />
          <BundleList title="可调用 Agents" items={p.activeAgents} empty="无" />
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

function bundleBadges(plugin: PluginView): Array<{ label: string; tone: string }> {
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

function McpView(props: {
  servers: MCPServerView[]
  allServers: MCPServerView[]
  query: string
  setQuery: (v: string) => void
  filter: McpFilter
  setFilter: (v: McpFilter) => void
  onConfigure?: () => void
}) {
  const { servers, allServers, query, setQuery, filter, setFilter, onConfigure } = props
  const configCount = allServers.filter((s) => s.plugin === '配置').length
  const readyCount = allServers.filter((s) => s.status === 'ready').length
  const errorCount = allServers.filter((s) => s.status === 'error').length
  return (
    <section className="overflow-hidden rounded-md border border-border/50 bg-surface">
      <div className="border-b border-border/45 px-4 py-3">
        <div className="mb-3 flex items-center justify-between gap-3">
          <div>
            <h3 className="text-sm font-semibold text-text">MCP</h3>
            <p className="mt-0.5 text-[11px] text-text-3">
              共 {allServers.length} 个，可用 {readyCount} 个，异常 {errorCount} 个，配置来源 {configCount} 个
            </p>
          </div>
          <button type="button" className="primary-button gap-1.5" onClick={onConfigure}>
            <GearIcon />
            配置
          </button>
        </div>
        <div className="flex flex-col gap-2 lg:flex-row lg:items-center">
          <div className="relative min-w-0 flex-1">
            <SearchIcon />
            <input
              className="field h-8 pl-8"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="搜索 server 名称、来源插件或命令"
            />
          </div>
          <div className="shrink-0">
            <SegmentedFilter
              value={filter}
              options={[
                { value: 'all', label: '全部' },
                { value: 'enabled', label: '可用' },
                { value: 'disabled', label: '停用' },
                { value: 'config', label: '配置' },
                { value: 'plugin', label: '插件' },
              ]}
              onChange={(value) => setFilter(value as McpFilter)}
            />
          </div>
        </div>
      </div>
      <ResultStrip shown={servers.length} total={allServers.length} query={query} extra={`${readyCount} 可用`} onClear={() => setQuery('')} />
      <div className="max-h-[calc(100vh-250px)] min-h-[360px] divide-y divide-border/40 overflow-y-auto scrollbar-gutter-stable">
        {servers.map((srv) => (
          <McpRow key={`${srv.plugin}/${srv.name}`} server={srv} />
        ))}
        {servers.length === 0 && <EmptyRows title="没有匹配的 MCP server" detail="清除搜索或切换来源过滤。" onClear={() => setQuery('')} />}
      </div>
    </section>
  )
}

function McpRow({ server }: { server: MCPServerView }) {
  const cmd = server.url || [server.command, ...(server.args ?? [])].join(' ')
  return (
    <div className="grid gap-2 px-4 py-3 transition-colors hover:bg-surface-3/35 md:grid-cols-[minmax(0,1fr)_auto] md:items-center">
      <div className="min-w-0">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <span className="truncate text-sm font-medium tracking-wide text-text">{server.name}</span>
          <StatusBadge status={server.status} />
          <span className="shrink-0 rounded-sm bg-accent/12 px-1.5 py-0.5 text-[10px] text-accent">{server.plugin}</span>
          {server.status === 'ready' && (
            <span className="shrink-0 rounded-sm bg-success/10 px-1.5 py-0.5 text-[10px] text-success">
              {server.toolCount ?? 0} tools
            </span>
          )}
          {!server.pluginEnabled && <span className="shrink-0 rounded-sm bg-surface-3 px-1.5 py-0.5 text-[10px] text-text-3">插件已停用</span>}
        </div>
        <p className="mt-1 line-clamp-1 font-mono text-[11px] text-text-3">{cmd || '未配置命令'}</p>
        {server.status === 'error' && server.error && (
          <p className="mt-1 line-clamp-2 text-[11px] text-danger">{server.error}</p>
        )}
      </div>
    </div>
  )
}

function StatusBadge({ status }: { status?: string }) {
  const label = status || 'unknown'
  return (
    <span className={cn('shrink-0 rounded-sm px-1.5 py-0.5 text-[10px]', statusTone(label))}>
      {label}
    </span>
  )
}

function statusTone(status: string): string {
  if (status === 'ready') return 'bg-success/12 text-success'
  if (status === 'error') return 'bg-danger/15 text-danger'
  if (status === 'starting' || status === 'authorizing') return 'bg-primary/12 text-primary'
  if (status === 'disabled') return 'bg-surface-3 text-text-3'
  return 'bg-warning/12 text-warning'
}

function ResultStrip({ shown, total, query, extra, onClear }: { shown: number; total: number; query: string; extra?: string; onClear: () => void }) {
  const hasQuery = query.trim().length > 0
  return (
    <div className="flex min-h-8 items-center gap-2 border-b border-border/40 px-3 text-[11px] text-text-3">
      <span className="min-w-0 flex-1 truncate">
        显示 {shown} / {total}
        {extra ? ` · ${extra}` : ''}
        {hasQuery ? ` · 搜索 "${query.trim()}"` : ''}
      </span>
      {hasQuery && (
        <button type="button" className="rounded-sm px-1.5 py-0.5 text-text-2 hover:bg-surface-3 hover:text-text" onClick={onClear}>
          清空
        </button>
      )}
    </div>
  )
}

function HeaderStat({ label, value }: { label: string; value: number | string }) {
  return (
    <span className="status-pill">
      <span className="mr-1 text-text-2">{value}</span>
      {label}
    </span>
  )
}

function EmptyRows({ title, detail, onClear }: { title: string; detail: string; onClear: () => void }) {
  return (
    <div className="px-4 py-10 text-center">
      <div className="mx-auto mb-3 flex h-9 w-9 items-center justify-center rounded-md bg-surface-3 text-text-3">
        <SearchEmptyIcon />
      </div>
      <div className="text-xs font-semibold text-text-2">{title}</div>
      <div className="mt-1 text-[11px] text-text-3">{detail}</div>
      <button type="button" className="secondary-button mt-3 h-7 px-2.5" onClick={onClear}>
        清除搜索
      </button>
    </div>
  )
}

function KeyValue({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="min-w-0">
      <div className="mb-0.5 font-medium text-text-3">{label}</div>
      <div className={cn('break-words text-text-2', mono && 'break-all font-mono')}>{value}</div>
    </div>
  )
}

function PluginSwitch({ enabled, busy, onClick }: { enabled: boolean; busy: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      className={cn(
        'inline-flex h-8 w-[112px] shrink-0 items-center justify-between rounded-md border px-2 text-xs font-semibold transition-all active:scale-95 disabled:cursor-wait disabled:opacity-65 disabled:active:scale-100',
        enabled ? 'border-success/25 bg-success/10 text-success hover:bg-success/15' : 'border-border/60 bg-surface-3 text-text-3 hover:text-text-2',
      )}
      disabled={busy}
      onClick={onClick}
      aria-pressed={enabled}
    >
      <span className="w-11 text-left">{busy ? '处理中' : enabled ? '已启用' : '已停用'}</span>
      <span
        className={cn(
          'relative h-4 w-8 shrink-0 overflow-hidden rounded-full transition-colors',
          enabled ? 'bg-success/30' : 'bg-border',
          busy && 'animate-pulse-soft',
        )}
        aria-hidden
      >
        <span
          className={cn(
            'absolute left-0.5 top-0.5 h-3 w-3 rounded-full bg-current transition-transform',
            enabled ? 'translate-x-4 text-success' : 'translate-x-0 text-text-3',
          )}
        />
      </span>
    </button>
  )
}

function SegmentedFilter({
  value,
  options,
  onChange,
}: {
  value: string
  options: Array<{ value: string; label: string }>
  onChange: (value: string) => void
}) {
  return (
    <div className="flex flex-wrap gap-1 rounded-md bg-surface-2 p-1">
      {options.map((option) => (
        <button
          key={option.value}
          type="button"
          className={cn(
            'h-7 rounded px-2 text-[11px] font-medium transition-all active:scale-95',
            value === option.value ? 'bg-primary text-black' : 'text-text-3 hover:bg-surface-3 hover:text-text',
          )}
          onClick={() => onChange(option.value)}
        >
          {option.label}
        </button>
      ))}
    </div>
  )
}

function SourcePill({ skill }: { skill: SkillView }) {
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

function SearchEmptyIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <circle cx="11" cy="11" r="7" />
      <path d="m20 20-3.5-3.5" />
      <path d="m8.5 8.5 5 5" />
      <path d="m13.5 8.5-5 5" />
    </svg>
  )
}

function GearIcon() {
  return (
    <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <path d="M12 15.5a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7Z" />
      <path d="M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.9-.3 1.7 1.7 0 0 0-1 1.6V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1-1.6 1.7 1.7 0 0 0-1.9.3l-.1.1A2 2 0 1 1 4.2 17l.1-.1a1.7 1.7 0 0 0 .3-1.9 1.7 1.7 0 0 0-1.6-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.6-1 1.7 1.7 0 0 0-.3-1.9l-.1-.1A2 2 0 1 1 7 4.2l.1.1a1.7 1.7 0 0 0 1.9.3h.1A1.7 1.7 0 0 0 10 3V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.6h.1a1.7 1.7 0 0 0 1.9-.3l.1-.1A2 2 0 1 1 19.8 7l-.1.1a1.7 1.7 0 0 0-.3 1.9v.1A1.7 1.7 0 0 0 21 10h.1a2 2 0 1 1 0 4H21a1.7 1.7 0 0 0-1.6 1Z" />
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
