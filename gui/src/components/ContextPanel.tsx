import { cn } from '../lib/classnames'
import type { ContextSegment, ContextSnapshot } from '../types/context'

interface ContextPanelProps {
  open: boolean
  snapshot: ContextSnapshot | null
  loading: boolean
  onClose: () => void
}

const toneClass: Record<string, string> = {
  muted: 'bg-text-3',
  blue: 'bg-primary',
  orange: 'bg-warning',
  yellow: 'bg-warning/70',
  violet: 'bg-accent',
  free: 'bg-surface-3',
}

export function ContextPanel({ open, snapshot, loading, onClose }: ContextPanelProps) {
  if (!open) return null

  const percent = snapshot ? Math.round(snapshot.percentUsed * 100) : 0

  return (
    <div className="fixed inset-0 z-40 flex justify-end bg-black/35" onMouseDown={onClose}>
      <aside
        className="flex h-full w-full max-w-[660px] flex-col border-l border-border/70 bg-surface-2 surface-shadow animate-slide-in"
        onMouseDown={(e) => e.stopPropagation()}
      >
        <header className="flex min-h-[60px] items-center gap-3 border-b border-border/60 px-5">
          <div className="min-w-0 flex-1">
            <h2 className="text-sm font-semibold text-text">上下文状态</h2>
            <p className="mt-0.5 text-[11px] text-text-3">
              {snapshot ? `${formatTokens(snapshot.used)} / ${formatTokens(snapshot.budget)} · ${percent}% 已用` : '读取上下文...'}
            </p>
          </div>
          <button className="icon-button" type="button" title="关闭" aria-label="关闭上下文状态" onClick={onClose}>
            <CloseIcon />
          </button>
        </header>

        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
          {loading ? (
            <div className="rounded-md border border-border/45 bg-surface px-4 py-3 text-sm text-text-2">正在读取上下文...</div>
          ) : snapshot ? (
            <div className="space-y-4">
              <section className="rounded-md border border-border/50 bg-surface px-4 py-3">
                <div className="mb-3 flex items-end justify-between gap-3">
                  <div>
                    <div className="text-[11px] font-medium uppercase tracking-wide text-text-3">Context window</div>
                    <div className="mt-1 text-xl font-semibold tabular-nums text-text">{percent}%</div>
                  </div>
                  <div className="text-right text-[11px] leading-relaxed text-text-3">
                    <div>剩余 {formatTokens(snapshot.free)}</div>
                    <div>预算 {formatTokens(snapshot.budget)}</div>
                  </div>
                </div>
                <SegmentBar snapshot={snapshot} />
              </section>

              <section className="grid gap-2 sm:grid-cols-3">
                <Metric label="工具" value={`${snapshot.toolDefCount}`} detail={formatTokens(snapshot.toolDefTokens)} />
                <Metric label="消息" value={`${snapshot.messageCount}`} detail={formatTokens(snapshot.messageTokens)} />
                <Metric label="归档" value={`${snapshot.archived}`} detail={`${snapshot.compactCount} 次压缩`} />
              </section>

              <section className="rounded-md border border-border/50 bg-surface px-4 py-2">
                {snapshot.segments.filter((segment) => segment.tokens > 0 || segment.key === 'free').map((segment) => (
                  <SegmentRow key={segment.key} segment={segment} budget={snapshot.budget} />
                ))}
              </section>

              <section className="grid gap-2 sm:grid-cols-2">
                <Metric label="缓存命中" value={formatTokens(snapshot.cacheHitTokens)} detail={`${Math.round(snapshot.cacheHitRatio * 100)}% hit`} />
                <Metric label="子代理" value={`${snapshot.subCount}`} detail={formatTokens(snapshot.subTokens)} />
              </section>

              {snapshot.governance && (
                <section className="rounded-md border border-border/50 bg-surface px-4 py-3">
                  <div className="mb-1 text-[11px] font-medium uppercase tracking-wide text-text-3">Governance</div>
                  <p className="text-[12px] leading-relaxed text-text-2">{snapshot.governance}</p>
                </section>
              )}
            </div>
          ) : (
            <div className="rounded-md border border-border/45 bg-surface px-4 py-3 text-sm text-text-2">暂无上下文信息</div>
          )}
        </div>
      </aside>
    </div>
  )
}

function SegmentBar({ snapshot }: { snapshot: ContextSnapshot }) {
  const denominator = Math.max(snapshot.budget, snapshot.used, 1)
  return (
    <div className="flex h-3 overflow-hidden rounded-sm bg-surface-3">
      {snapshot.segments.map((segment) => {
        const width = Math.max(segment.tokens / denominator * 100, segment.tokens > 0 ? 1.2 : 0)
        return (
          <div
            key={segment.key}
            className={cn('h-full', toneClass[segment.tone] ?? 'bg-text-3')}
            style={{ width: `${width}%` }}
            title={`${segment.label}: ${formatTokens(segment.tokens)}`}
          />
        )
      })}
    </div>
  )
}

function SegmentRow({ segment, budget }: { segment: ContextSegment; budget: number }) {
  const pct = budget > 0 ? Math.round(segment.tokens / budget * 100) : 0
  return (
    <div className="flex items-center gap-3 border-b border-border/35 py-2 last:border-b-0">
      <span className={cn('h-2.5 w-2.5 rounded-full', toneClass[segment.tone] ?? 'bg-text-3')} />
      <span className="min-w-0 flex-1 text-[12px] font-medium text-text">{segment.label}</span>
      <span className="font-mono text-[11px] tabular-nums text-text-2">{formatTokens(segment.tokens)}</span>
      <span className="w-10 text-right font-mono text-[11px] tabular-nums text-text-3">{pct}%</span>
    </div>
  )
}

function Metric({ label, value, detail }: { label: string; value: string; detail: string }) {
  return (
    <div className="rounded-md border border-border/50 bg-surface px-3 py-2">
      <div className="text-[11px] text-text-3">{label}</div>
      <div className="mt-1 text-[15px] font-semibold tabular-nums text-text">{value}</div>
      <div className="mt-0.5 truncate font-mono text-[10.5px] text-text-3">{detail}</div>
    </div>
  )
}

function formatTokens(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return '0'
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`
  return `${Math.round(n)}`
}

function CloseIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" aria-hidden>
      <path d="M18 6 6 18" />
      <path d="m6 6 12 12" />
    </svg>
  )
}
