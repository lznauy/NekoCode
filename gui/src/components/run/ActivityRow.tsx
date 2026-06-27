// ActivityRow: 一行工具步骤。
// 颜色胶带左侧细条标识状态, 内容区与工具行同列对齐, 不产生独立子框。
import { memo, useCallback, useMemo, useRef } from 'react'
import type { ToolStep } from '../../types/events'
import { useScrollContainer } from '../MessageList'
import { compactArgs, editSummary, pathFromArgs, prettyTool } from './helpers'
import { EditDiff } from './EditDiff'

interface ActivityRowProps {
  step: ToolStep
  toggleStep: (stepId: string) => void
}

// 状态 → 左侧细条颜色 (2px 胶带, 不占满全高)
function statusTape(s: ToolStep): string {
  if (s.isError) return 'bg-danger/70'
  switch (s.status) {
    case 'running': return 'bg-primary'
    case 'done':    return 'bg-success/70'
    case 'blocked': return 'bg-warning/70'
    default:        return 'bg-text-3/30'
  }
}

export const ActivityRow = memo(function ActivityRow({ step, toggleStep }: ActivityRowProps) {
  const rowRef = useRef<HTMLDivElement>(null)
  const scrollRef = useScrollContainer()
  const expanded = !step.collapsed
  // edit 成功后保留 preview diff; 运行中显示 preview; 其余完成状态显示 output。
  const content = step.toolName === 'edit'
    ? (step.preview || step.output || '')
    : step.status === 'running'
      ? (step.preview || '')
      : (step.output || '')
  const canExpand = !!content

  const handleToggle = useCallback(() => {
    if (!canExpand) return
    const rowEl = rowRef.current
    const scrollEl = scrollRef.current
    let offsetBefore = 0
    if (rowEl && scrollEl) {
      offsetBefore = rowEl.getBoundingClientRect().top - scrollEl.getBoundingClientRect().top
    }
    toggleStep(step.id)
    if (rowEl && scrollEl) {
      requestAnimationFrame(() => {
        requestAnimationFrame(() => {
          const offsetAfter = rowEl.getBoundingClientRect().top - scrollEl.getBoundingClientRect().top
          const delta = offsetAfter - offsetBefore
          if (delta !== 0) {
            scrollEl.scrollTop += delta
          }
        })
      })
    }
  }, [canExpand, toggleStep, step.id, scrollRef])

  // hook 顺序: useMemo 必须无条件调用。
  const argsLabel = useMemo(() => compactArgs(step.args), [step.args])
  const editSum = useMemo(() => editSummary(content), [content])

  // 状态仅通过颜色表达 — 不再单独放置一个圆点/勾 glyph。
  const badgeCls = step.isError
    ? 'text-danger'
    : step.status === 'running'
      ? 'text-primary' // 去除 animate-pulse-soft 避免持续合成重绘
      : step.status === 'done'
        ? 'text-success'
        : step.status === 'blocked'
          ? 'text-warning'
          : 'text-text-3'

  const tape = statusTape(step)

  return (
    <div
      ref={rowRef}
      className={`flex flex-col overflow-hidden rounded-lg ${expanded ? 'bg-surface-2/70' : 'bg-surface-2/40'}`}
    >
      {/* 状态胶带 + 工具行 */}
      <div className="flex items-stretch">
        <span className={`w-[2px] shrink-0 ${tape}`} aria-hidden />
        <button
          type="button"
          onClick={handleToggle}
          disabled={!canExpand}
          className={`group flex flex-1 items-center gap-2 px-2.5 py-1.5 text-left text-[12px] ${
            canExpand ? 'hover:bg-surface-3/50' : 'cursor-default'
          }`}
        >
          {/* 展开指示器 或 占位 */}
          <span className="w-3 text-center leading-none text-text-3 text-[10px]">
            {canExpand ? (expanded ? '▾' : '▸') : ' '}
          </span>
          <span className={`flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-surface text-[13px] leading-none ${badgeCls}`}>
            <ToolGlyph name={step.toolName} />
          </span>
          <span className={`font-medium ${step.isError ? 'text-danger' : 'text-text-2'}`}>
            {prettyTool(step.toolName)}
          </span>
          {argsLabel && (
            <span
              className={`truncate font-mono text-[11px] ${
                step.toolName === 'bash' ? 'text-text-2' : 'text-text-3'
              }`}
            >
              {argsLabel}
            </span>
          )}
          {step.toolName === 'edit' && editSum && (
            <span className="font-mono text-[11px] text-success">{editSum}</span>
          )}
        </button>
      </div>
      {expanded && content && <RowBody step={step} />}
    </div>
  )
})

function ToolGlyph({ name }: { name: string }) {
  const common = { width: 14, height: 14, viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', strokeWidth: 2.1, strokeLinecap: 'round' as const, strokeLinejoin: 'round' as const, 'aria-hidden': true }
  switch (name) {
    case 'read':
    case 'tsread':
      return <svg {...common}><path d="M4 19.5V5a2 2 0 0 1 2-2h11a1 1 0 0 1 1 1v16H6a2 2 0 0 1-2-2Z" /><path d="M8 7h6M8 11h7" /></svg>
    case 'edit':
      return <svg {...common}><path d="M12 20h9" /><path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4Z" /></svg>
    case 'write':
      return <svg {...common}><path d="M5 4h10l4 4v12H5Z" /><path d="M14 4v5h5" /><path d="M8 14h8M8 17h5" /></svg>
    case 'bash':
      return <svg {...common}><path d="m7 8 4 4-4 4" /><path d="M13 16h4" /></svg>
    case 'grep':
    case 'glob':
    case 'searchfiles':
      return <svg {...common}><circle cx="10.5" cy="10.5" r="5.5" /><path d="m15 15 5 5" /></svg>
    case 'todo':
      return <svg {...common}><path d="m4 7 2 2 4-4" /><path d="M12 8h8" /><path d="m4 17 2 2 4-4" /><path d="M12 18h8" /></svg>
    case 'webfetch':
    case 'fetch':
      return <svg {...common}><circle cx="12" cy="12" r="9" /><path d="M3 12h18" /><path d="M12 3a14 14 0 0 1 0 18" /><path d="M12 3a14 14 0 0 0 0 18" /></svg>
    case 'think':
      return <svg {...common}><path d="M8 14a5 5 0 1 1 8 0c-.7.6-1 1.3-1 2H9c0-.7-.3-1.4-1-2Z" /><path d="M9 20h6" /></svg>
    default:
      return <svg {...common}><path d="M12 3v18M3 12h18" /></svg>
  }
}

function RowBody({ step }: { step: ToolStep }) {
  // edit 成功后保留 preview diff; 运行中显示 preview; 其余完成状态显示 output。
  const content = step.toolName === 'edit'
    ? (step.preview || step.output || '')
    : step.status === 'running'
      ? (step.preview || '')
      : (step.output || '')
  if (step.toolName === 'edit' && step.isError) {
    return (
      <div className="border-t border-danger/20 px-3 pb-2 pt-2 font-mono text-[11.5px] leading-relaxed text-danger whitespace-pre-wrap">
        {content || 'edit failed'}
      </div>
    )
  }
  if (step.toolName === 'edit' && isEditRevertOutput(content)) {
    return (
      <div className="border-t border-success/20 px-3 pb-2 pt-2 font-mono text-[11.5px] leading-relaxed text-text-2 whitespace-pre-wrap">
        {content}
      </div>
    )
  }
  if (step.toolName === 'edit') {
    return <EditDiff content={content} filePath={pathFromArgs(step.args)} defaultCollapsed={false} skipHeader />
  }
  if (step.isError) {
    return (
      <div className="border-t border-danger/20 px-3 pb-2 pt-2 font-mono text-[11.5px] leading-relaxed text-danger whitespace-pre-wrap">
        {content}
      </div>
    )
  }
  const scrollable = step.toolName !== 'write'
  return (
    <pre
      className={`border-t border-border/30 px-3 pb-2 pt-2 font-mono text-[11.5px] leading-relaxed text-text-2 [overflow-wrap:break-word] ${
        scrollable ? 'max-h-[320px] overflow-y-auto' : ''
      }`}
    >
      {content}
    </pre>
  )
}

function isEditRevertOutput(content: string): boolean {
  return content.includes('Reverted to pre-edit state')
}
