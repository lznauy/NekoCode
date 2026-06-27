// RunCard: 一次 assistant run 的"工作卡"。
// 性能与视觉平衡: 保留 rounded/border/bg (视觉边界), 去除 shadow-sm;
// 运行态只让 13px 状态图标旋转, 避免整块卡片参与持续重绘。
import { memo } from 'react'
import type { Msg } from '../../types/events'
import { MarkdownBody } from '../MarkdownBody'
import { ActivityRow } from './ActivityRow'
import { ImageGrid } from './ImageGrid'
import { TasksList } from './TasksList'
import { ThinkingCard } from './ThinkingCard'

const PERSISTENT = new Set(['edit', 'bash', 'write'])
const persistentTool = (name: string) => PERSISTENT.has(name)

interface RunCardProps {
  msg: Msg
  toggleStep: (stepId: string) => void
}

const PHASE_LABEL: Record<NonNullable<Msg['phase']>, string> = {
  ready: '就绪',
  waiting: '待机…',
  thinking: '思考中',
  reasoning: '组织回答',
  running: '使用工具',
}

export const RunCard = memo(function RunCard({ msg, toggleStep }: RunCardProps) {
  const streaming = msg.streaming
  const phase = msg.phase ?? (streaming ? 'thinking' : 'ready')
  // 流式中显示全部工具步骤；结束后只保留持久化工具（edit/bash/write），与 sessionview 一致。
  const allSteps = msg.steps ?? []
  const steps = streaming ? allSteps : allSteps.filter((s) => persistentTool(s.toolName))
  const toolCount = allSteps.length
  const persistCount = allSteps.filter((s) => persistentTool(s.toolName)).length
  const activity = msg.activity
  const tokenPrompt = msg.tokens?.prompt ?? 0
  const tokenCompl = msg.tokens?.completion ?? 0

  return (
    <div className="flex flex-col gap-2 rounded-xl border border-border/70 bg-surface p-4">
      {/* —— Header —— */}
      <header className="flex items-center gap-2 text-[12px] text-text-2">
        {streaming ? (
          <RunSpinner />
        ) : (
          <span className="flex h-5 w-5 items-center justify-center rounded-md bg-success/12 text-success" aria-hidden>
            <CheckIcon />
          </span>
        )}
        <span className="font-medium text-text">{PHASE_LABEL[phase]}</span>
        {(tokenPrompt > 0 || tokenCompl > 0) && (
          <span className="font-mono text-[10.5px] text-text-3 tabular-nums">
            ↑{fmt(tokenPrompt)} ↓{fmt(tokenCompl)}
          </span>
        )}
        {toolCount > 0 && <span className="text-text-3">· {toolCount} 关键工具</span>}
        {activity && activity.reads > 0 && <span className="text-text-3">· 读取 {activity.reads}</span>}
        {activity && activity.searches > 0 && <span className="text-text-3">· 搜索 {activity.searches}</span>}
        {activity && activity.fetches > 0 && <span className="text-text-3">· 网页 {activity.fetches}</span>}
        {persistCount > 0 && <span className="text-text-3">· {persistCount} 改动</span>}
        {(msg.compactCount ?? 0) > 0 && (
          <span className="text-text-3">· compact {msg.compactCount}</span>
        )}
        {msg.subagents && msg.subagents.length > 0 && (
          <span className="ml-1 inline-flex h-5 items-center gap-1 rounded-md bg-accent/10 px-1.5 text-[10px] text-accent">
            <BranchIcon />
            并行 {msg.subagents.length}
          </span>
        )}
      </header>

      {/* —— Tasks —— */}
      {streaming && msg.todos && msg.todos.length > 0 && <TasksList todos={msg.todos} />}

      {/* —— 工具步骤 —— */}
      {toolCount > 0 && (
        <div className="flex flex-col gap-1">
          {steps.map((s) => (
            <ActivityRow key={s.id} step={s} toggleStep={toggleStep} />
          ))}
        </div>
      )}

      {/* —— 生成图片 —— */}
      {msg.images && msg.images.length > 0 && <ImageGrid images={msg.images} />}

      {/* —— reasoning —— */}
      <ThinkingCard reasoning={msg.reasoning ?? ''} done={!!msg.reasoningDone} />

      {/* —— output —— */}
      {streaming && msg.streamText && <TransientOutput text={msg.streamText} />}
      {msg.text ? (
        <div className="min-w-0 text-sm leading-relaxed text-text [overflow-wrap:break-word]">
          {streaming ? <StreamText text={msg.text} /> : <MarkdownBody text={msg.text} />}
        </div>
      ) : streaming ? (
        // 占位横线: header 已表达运行态, 这里只做视觉铺垫, 不重复文字标签。
        <div className="h-px w-[120px] bg-border/40" />
      ) : null}
    </div>
  )
})

function StreamText({ text }: { text: string }) {
  return <div className="whitespace-pre-wrap [overflow-wrap:break-word]">{text}</div>
}

function TransientOutput({ text }: { text: string }) {
  const lines = tailContent(text, 6)
  if (!lines.length) return null
  return (
    <div className="rounded-md bg-surface-2/55 px-3 py-2 text-[12px] leading-relaxed text-text-2">
      <div className="mb-1 text-[10px] font-medium uppercase tracking-[0.14em] text-text-3">临时输出</div>
      <div className="whitespace-pre-wrap [overflow-wrap:break-word]">{lines.join('\n')}</div>
    </div>
  )
}

function tailContent(text: string, maxLines: number): string[] {
  const normalized = text.replace(/\r\n/g, '\n').replace(/\r/g, '\n').trimEnd()
  if (!normalized.trim()) return []
  const lines = normalized
    .split('\n')
    .map((line) => line.trimEnd())
    .filter((line) => !isNoise(line) && line.trim() !== '')
  return lines.slice(-maxLines)
}

function isNoise(line: string): boolean {
  const s = line.trim()
  return s !== '' && !/[A-Za-z0-9\u0080-\uFFFF]/.test(s)
}

function RunSpinner() {
  return (
    <span className="inline-flex h-5 w-5 items-center justify-center rounded-md bg-primary/12 text-primary" aria-hidden>
      <svg className="animate-spin" width="13" height="13" viewBox="0 0 24 24" fill="none">
        <path d="M12 3a9 9 0 1 1-6.4 2.7" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" />
      </svg>
    </span>
  )
}

function CheckIcon() {
  return (
    <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" aria-hidden>
      <path d="m5 12 4 4L19 6" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

function BranchIcon() {
  return (
    <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" aria-hidden>
      <path d="M6 3v6a3 3 0 0 0 3 3h9" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M6 21v-6a3 3 0 0 1 3-3" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

function fmt(n: number): string {
  if (n >= 1000) return (n / 1000).toFixed(1) + 'k'
  return String(n)
}
