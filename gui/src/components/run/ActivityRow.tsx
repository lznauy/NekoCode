// ActivityRow: 一行工具步骤。
// 颜色胶带左侧细条标识状态, 内容区与工具行同列对齐, 不产生独立子框。
import { useState } from 'react'
import type { ToolStep } from '../../types/events'
import { compactArgs, editSummary, prettyTool, toolIcon } from './helpers'
import { EditDiff } from './EditDiff'

interface ActivityRowProps {
  step: ToolStep
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

export function ActivityRow({ step }: ActivityRowProps) {
  // edit / bash 默认展开, 其余工具默认折叠。
  const [expanded, setExpanded] = useState(
    step.toolName === 'edit' || step.toolName === 'bash',
  )
  // edit 成功后保留 preview diff; 运行中显示 preview; 其余完成状态显示 output。
  const content = step.toolName === 'edit'
    ? (step.preview || step.output || '')
    : step.status === 'running'
      ? (step.preview || '')
      : (step.output || '')
  const canExpand = !!content

  // 状态仅通过颜色表达 — 不再单独放置一个圆点/勾 glyph。
  const badgeCls = step.isError
    ? 'text-danger'
    : step.status === 'running'
      ? 'text-primary animate-pulse-soft'
      : step.status === 'done'
        ? 'text-success'
        : step.status === 'blocked'
          ? 'text-warning'
          : 'text-text-3'

  const tape = statusTape(step)

  return (
    <div
      className={`flex flex-col overflow-hidden rounded-lg transition-colors ${expanded ? 'bg-surface-2/70' : 'bg-surface-2/40'}`}
    >
      {/* 状态胶带 + 工具行 */}
      <div className="flex items-stretch">
        <span className={`w-[2px] shrink-0 ${tape}`} aria-hidden />
        <button
          type="button"
          onClick={() => canExpand && setExpanded((v) => !v)}
          disabled={!canExpand}
          className={`group flex flex-1 items-center gap-2 px-2.5 py-1.5 text-left text-[12px] transition-colors ${
            canExpand ? 'hover:bg-surface-3/50' : 'cursor-default'
          }`}
        >
          {/* 展开指示器 或 占位 */}
          <span className="w-3 text-center leading-none text-text-3 text-[10px]">
            {canExpand ? (expanded ? '▾' : '▸') : ' '}
          </span>
          {/* 工具 emoji — 用状态色渲染 */}
          <span className={`shrink-0 text-[13px] leading-none ${badgeCls}`}>
            {toolIcon(step.toolName)}
          </span>
          <span className={`font-medium ${step.isError ? 'text-danger' : 'text-text-2'}`}>
            {prettyTool(step.toolName)}
          </span>
          {compactArgs(step.args) && (
            <span
              className={`truncate font-mono text-[11px] ${
                step.toolName === 'bash' ? 'text-text-2' : 'text-text-3'
              }`}
            >
              {compactArgs(step.args)}
            </span>
          )}
          {step.toolName === 'edit' && editSummary(content) && (
            <span className="font-mono text-[11px] text-success">{editSummary(content)}</span>
          )}
        </button>
      </div>
      {expanded && content && <RowBody step={step} />}
    </div>
  )
}

function RowBody({ step }: { step: ToolStep }) {
  // edit 成功后保留 preview diff; 运行中显示 preview; 其余完成状态显示 output。
  const content = step.toolName === 'edit'
    ? (step.preview || step.output || '')
    : step.status === 'running'
      ? (step.preview || '')
      : (step.output || '')
  if (step.toolName === 'edit') {
    return <EditDiff content={content} filePath={step.args} defaultCollapsed={false} skipHeader />
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
