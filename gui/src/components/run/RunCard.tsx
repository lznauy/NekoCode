// RunCard: 一次 assistant run 的"工作卡"。
// 设计: 单一容器 (rounded-2xl + bg-surface-2 + border + p-4), 不再嵌套边框层;
// 工具步骤用 ActivityRow 的状态胶带行做分层, 不用左边竖线子框。
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
}

const PHASE_LABEL: Record<NonNullable<Msg['phase']>, string> = {
  ready: '就绪',
  waiting: '待机…',
  thinking: '思考中',
  reasoning: '组织回答',
  running: '使用工具',
}

export function RunCard({ msg }: RunCardProps) {
  const streaming = msg.streaming
  const phase = msg.phase ?? (streaming ? 'thinking' : 'ready')
  // 流式中显示全部工具步骤；结束后只保留持久化工具（edit/bash/write），与 sessionview 一致。
  const allSteps = msg.steps ?? []
  const steps = streaming ? allSteps : allSteps.filter((s) => persistentTool(s.toolName))
  const toolCount = allSteps.length
  const persistCount = allSteps.filter((s) => persistentTool(s.toolName)).length
  const tokenPrompt = msg.tokens?.prompt ?? 0
  const tokenCompl = msg.tokens?.completion ?? 0

  return (
    <div className="flex flex-col gap-2 rounded-xl border border-border/70 bg-surface p-4 shadow-sm">
      {/* —— Header —— 一条贴边横线左侧的小标签栏 */}
      <header className="flex items-center gap-2 text-[12px] text-text-2">
        {streaming ? (
          <RunSpinner />
        ) : (
          <span className="h-2 w-2 rounded-full bg-success" aria-hidden />
        )}
        <span className="font-medium text-text">{PHASE_LABEL[phase]}</span>
        {(tokenPrompt > 0 || tokenCompl > 0) && (
          <span className="font-mono text-[10.5px] text-text-3 tabular-nums">
            ↑{fmt(tokenPrompt)} ↓{fmt(tokenCompl)}
          </span>
        )}
        {toolCount > 0 && <span className="text-text-3">· {toolCount} 工具</span>}
        {persistCount > 0 && <span className="text-text-3">· {persistCount} 改动</span>}
        {(msg.compactCount ?? 0) > 0 && (
          <span className="text-text-3">· compact {msg.compactCount}</span>
        )}
        {msg.subagents && msg.subagents.length > 0 && (
          <span className="ml-1 flex items-center gap-1">
            {msg.subagents.map((s) => (
              <span
                key={s.id}
                className="inline-block h-1.5 w-1.5 rounded-full"
                style={{ background: SUBAGENT_COLORS[s.colorIdx % SUBAGENT_COLORS.length] }}
              />
            ))}
            <span className="text-[10px] text-text-3">并行</span>
          </span>
        )}
      </header>

      {/* —— Tasks —— */}
      {msg.todos && msg.todos.length > 0 && <TasksList todos={msg.todos} />}

      {/* —— 工具步骤 —— 用间距与缩进分层, 不围子框 */}
      {toolCount > 0 && (
        <div className="flex flex-col gap-1">
          {steps.map((s) => (
            <ActivityRow key={s.id} step={s} />
          ))}
        </div>
      )}

      {/* —— 生成图片 —— */}
      {msg.images && msg.images.length > 0 && <ImageGrid images={msg.images} />}

      {/* —— reasoning —— */}
      <ThinkingCard reasoning={msg.reasoning ?? ''} done={!!msg.reasoningDone} />

      {/* —— output —— */}
      {msg.text ? (
        <div className="min-w-0 text-sm leading-relaxed text-text [overflow-wrap:break-word]">
          <MarkdownBody text={msg.text} />
          {streaming && (
            <span className="ml-0.5 inline-block h-[1.1em] w-[1.5px] animate-blink rounded-sm bg-primary align-text-bottom" />
          )}
        </div>
      ) : streaming ? (
        <div className="flex items-center gap-1.5 text-[12px] text-text-3">
          <span className="h-1.5 w-1.5 rounded-full bg-primary animate-pulse-soft" />
          <span className="font-mono">[{PHASE_LABEL[phase]}]</span>
        </div>
      ) : null}
    </div>
  )
}

function RunSpinner() {
  return (
    <span className="inline-flex h-3.5 w-3.5 items-center justify-center" aria-hidden>
      <span className="h-2 w-2 rounded-full bg-primary animate-pulse-soft" />
    </span>
  )
}

const SUBAGENT_COLORS = ['#b989d8', '#e8a02e', '#7fb583', '#e89866']

function fmt(n: number): string {
  if (n >= 1000) return (n / 1000).toFixed(1) + 'k'
  return String(n)
}
