import { useEffect, useRef } from 'react'
import { safeEventsOn } from '../lib/wails'
import type {
  DeltaEvent,
  DoneEvent,
  StatusEvent,
  StepEvent,
  PhaseEvent,
  ReasoningEvent,
  ToolStartPayload,
  ToolPreviewPayload,
  ToolDonePayload,
  SubAgentStartPayload,
  SubAgentEndPayload,
  TodosPayload,
  MetricsPayload,
  ConfirmEvent,
} from '../types/events'

export interface AgentEventHandlers {
  onDelta: (e: DeltaEvent) => void
  onReasoning?: (e: ReasoningEvent) => void
  onPhase?: (e: PhaseEvent) => void
  onToolStart?: (e: ToolStartPayload) => void
  onToolPreview?: (e: ToolPreviewPayload) => void
  onToolDone?: (e: ToolDonePayload) => void
  onSubAgentStart?: (e: SubAgentStartPayload) => void
  onSubAgentEnd?: (e: SubAgentEndPayload) => void
  onTodos?: (e: TodosPayload) => void
  onMetrics?: (e: MetricsPayload) => void
  onStep: (e: StepEvent) => void
  onDone: (e: DoneEvent) => void
  onStatus: (e: StatusEvent) => void
  onConfirm?: (e: ConfirmEvent) => void
}

export function useWailsEvents(handlers: AgentEventHandlers): void {
  const ref = useRef(handlers)
  useEffect(() => {
    ref.current = handlers
  })

  useEffect(() => {
    const cleanups: (() => void)[] = []
    cleanups.push(safeEventsOn('agent:delta', (e: unknown) => ref.current.onDelta(e as DeltaEvent)))
    cleanups.push(safeEventsOn('agent:reasoning', (e: unknown) => ref.current.onReasoning?.(e as ReasoningEvent)))
    cleanups.push(safeEventsOn('agent:phase', (e: unknown) => ref.current.onPhase?.(e as PhaseEvent)))
    cleanups.push(safeEventsOn('agent:tool_start', (e: unknown) => ref.current.onToolStart?.(e as ToolStartPayload)))
    cleanups.push(safeEventsOn('agent:tool_preview', (e: unknown) => ref.current.onToolPreview?.(e as ToolPreviewPayload)))
    cleanups.push(safeEventsOn('agent:tool_done', (e: unknown) => ref.current.onToolDone?.(e as ToolDonePayload)))
    cleanups.push(safeEventsOn('agent:subagent_start', (e: unknown) => ref.current.onSubAgentStart?.(e as SubAgentStartPayload)))
    cleanups.push(safeEventsOn('agent:subagent_end', (e: unknown) => ref.current.onSubAgentEnd?.(e as SubAgentEndPayload)))
    cleanups.push(safeEventsOn('agent:todos', (e: unknown) => ref.current.onTodos?.(e as TodosPayload)))
    cleanups.push(safeEventsOn('agent:metrics', (e: unknown) => ref.current.onMetrics?.(e as MetricsPayload)))
    cleanups.push(safeEventsOn('agent:step', (e: unknown) => ref.current.onStep(e as StepEvent)))
    cleanups.push(safeEventsOn('agent:done', (e: unknown) => ref.current.onDone(e as DoneEvent)))
    cleanups.push(safeEventsOn('agent:status', (e: unknown) => ref.current.onStatus(e as StatusEvent)))
    cleanups.push(safeEventsOn('agent:confirm', (e: unknown) => ref.current.onConfirm?.(e as ConfirmEvent)))

    return () => {
      cleanups.forEach((fn) => {
        try {
          fn()
        } catch {
          /* ignore */
        }
      })
    }
  }, [])
}