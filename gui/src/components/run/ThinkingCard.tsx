// ThinkingCard: reasoning 折叠块。
// 默认收起; 顶栏极克制 (无大 border, 仅 summary 字号小化);
// 展开后内部独立滚动避免占用对话区。
import { useState } from 'react'
import { MarkdownBody } from '../MarkdownBody'

interface ThinkingCardProps {
  reasoning: string
  done: boolean
}

export function ThinkingCard({ reasoning, done }: ThinkingCardProps) {
  const [open, setOpen] = useState(false)
  if (!reasoning.trim()) return null

  return (
    <details
      open={open}
      onToggle={(e) => setOpen((e.target as HTMLDetailsElement).open)}
      className="group"
    >
      <summary className="flex cursor-pointer items-center gap-2 px-1 py-1 text-[11.5px] text-text-3 transition-colors select-none hover:text-text-2">
        <span className="leading-none">{open ? '▾' : '▸'}</span>
        <span className="text-accent/80">推理过程</span>
        {!done && <span className="text-primary animate-pulse-soft">●</span>}
        <span className="ml-auto font-mono text-[10px] tabular-nums">
          {reasoning.length > 600 ? `${(reasoning.length / 1000).toFixed(1)}k` : `${reasoning.length}c`}
        </span>
      </summary>
      <div className="mt-1 max-h-[240px] overflow-y-auto border-l-2 border-accent/30 pl-3 text-[12.5px] text-text-2">
        <MarkdownBody text={reasoning} />
      </div>
    </details>
  )
}
