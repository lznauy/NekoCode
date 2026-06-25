// TasksList: Todo 进度列表。
// 与卡片整体一致: 不再围 border, 用 border-l-warning/40 标识左侧色, 进度条主色继承。
import type { TodoItem } from '../../types/events'

interface TasksListProps {
  todos: TodoItem[]
}

export function TasksList({ todos }: TasksListProps) {
  if (!todos || todos.length === 0) return null
  const total = todos.length
  const done = todos.filter((t) => t.status === 'completed').length
  const pct = total ? Math.round((done / total) * 100) : 0

  return (
    <div className="flex flex-col gap-2 border-l-2 border-warning/40 pl-3">
      <div className="flex items-center gap-2 text-[12px]">
        <span className="text-warning">📋</span>
        <span className="font-medium text-text-2">任务</span>
        <span className="ml-auto font-mono text-[10px] text-text-3 tabular-nums">
          {done}/{total} · {pct}%
        </span>
      </div>
      <div className="h-1 overflow-hidden rounded-full bg-surface-3">
        <div className="h-full rounded-full bg-gradient-to-r from-warning to-success transition-all duration-500" style={{ width: `${pct}%` }} />
      </div>
      <ul className="flex flex-col gap-0.5">
        {todos.map((t, i) => (
          <li key={i} className="flex items-start gap-2 text-[12.5px]">
            <span className="mt-px w-3 leading-none">{statusGlyph(t.status)}</span>
            <span className={textClass(t.status)}>
              <span className="truncate">{t.content || '(empty)'}</span>
            </span>
          </li>
        ))}
      </ul>
    </div>
  )
}

function statusGlyph(s: TodoItem['status']): string {
  switch (s) {
    case 'completed':   return '✓'
    case 'in_progress': return '◐'
    case 'pending':     return '○'
  }
}

function textClass(s: TodoItem['status']): string {
  switch (s) {
    case 'completed':   return 'text-text-3 line-through'
    case 'in_progress': return 'text-primary font-medium'
    case 'pending':     return 'text-text-2'
  }
}