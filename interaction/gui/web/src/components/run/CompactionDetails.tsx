import { useEffect, useRef, useState } from 'react'
import type { CompactionEvent } from '../../types/events'

export function CompactionDetails({ event }: { event: CompactionEvent }) {
  const running = event.status === 'started' || event.status === 'delta'
  const [expanded, setExpanded] = useState(running || event.status === 'failed')
  const scrollRef = useRef<HTMLDivElement>(null)
  const followRef = useRef(true)
  useEffect(() => {
    if (running && expanded && followRef.current && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [event.summary, running, expanded])
  useEffect(() => {
    setExpanded(running || event.status === 'failed')
  }, [running, event.id, event.status === 'failed'])
  const title = running ? '正在压缩上下文' : event.status === 'failed' ? '上下文压缩未完成' : '上下文已压缩'
  const body = (event.summary ?? '').replace(/^\s*<summary>\s*/, '').replace(/\s*<\/summary>\s*$/, '')
  return (
    <section className="min-w-0 rounded-md bg-surface-2 px-3 py-1 text-text-2" aria-label="上下文压缩详情">
      <button type="button" aria-expanded={expanded} aria-controls={`summary-${event.id}`}
        onClick={() => setExpanded((value) => !value)}
        className="flex min-h-10 w-full min-w-0 items-center gap-2 rounded-md text-left text-[12px] transition-transform active:scale-95 focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary">
        <span aria-hidden className={running ? 'text-primary motion-safe:animate-pulse' : event.status === 'failed' ? 'text-danger' : 'text-success'}>{running ? '◌' : event.status === 'failed' ? '!' : '✓'}</span>
        <span className="font-medium">{title}</span>
        <span className="text-text-3">{event.trigger === 'manual' ? '手动' : '自动'}</span>
        <span className="ml-auto shrink-0 text-text-3" aria-hidden>{expanded ? '▾' : '▸'}</span>
      </button>
      <div className="pb-2 text-[11px] tabular-nums text-text-3">
        {event.beforeMessages} {event.status === 'completed' ? `→ ${event.afterMessages} ` : ''}条消息 · 约 {event.beforeTokens.toLocaleString()}{event.status === 'completed' ? ` → ${event.afterTokens.toLocaleString()}` : ''} tokens
        {!running && ` · ${(event.elapsedMs / 1000).toFixed(1)}s`}
      </div>
      {expanded && <div ref={scrollRef} id={`summary-${event.id}`} onScroll={(e) => {
        const node = e.currentTarget
        followRef.current = node.scrollHeight - node.scrollTop - node.clientHeight < 24
      }} className="max-h-80 overflow-y-auto overscroll-contain pb-3 text-[12px] leading-relaxed [overflow-wrap:anywhere]">
        {event.status === 'failed' && event.error && <p className="mb-2 text-danger">{event.error}</p>}
        {event.warning && <p className="mb-2 text-warning">{event.warning}</p>}
        <div className="whitespace-pre-wrap">{body || (running ? '正在生成摘要…' : '没有可用的摘要内容。')}</div>
      </div>}
    </section>
  )
}
