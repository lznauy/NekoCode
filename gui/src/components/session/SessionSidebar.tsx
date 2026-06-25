import { useState } from 'react'
import { cn } from '../../lib/classnames'
import type { Msg } from '../../types/events'
import type { SessionMeta } from '../../types/session'
import { NewSessionButton } from './NewSessionButton'
import { SessionItem } from './SessionItem'

interface SessionSidebarProps {
  sessions: SessionMeta[]
  currentId: string | null
  loading: boolean
  onCreate: () => void
  onSwitch: (id: string) => Promise<Msg[] | null>
  onDelete: (id: string) => void
}

export function SessionSidebar({
  sessions,
  currentId,
  loading,
  onCreate,
  onSwitch,
  onDelete,
}: SessionSidebarProps) {
  const [collapsed, setCollapsed] = useState(false)

  if (collapsed) {
    return (
      <aside className="flex h-full w-12 flex-col items-center gap-3 border-r border-border/80 bg-surface py-3">
        <NewSessionButton onClick={onCreate} collapsed />
        <button
          type="button"
          onClick={() => setCollapsed(false)}
          title="展开会话列表"
          className="rounded-md p-1.5 text-text-3 transition-all hover:bg-surface-3 hover:text-text active:scale-95"
        >
          <ChevronRightIcon />
        </button>
      </aside>
    )
  }

  return (
    <aside className="flex h-full w-[272px] flex-col border-r border-border/80 bg-surface">
      <div className="flex h-[52px] items-center justify-between px-3">
        <span className="text-[13px] font-semibold uppercase leading-none tracking-[0.14em] text-text-2">Sessions</span>
        <div className="flex items-center gap-1">
          <NewSessionButton onClick={onCreate} />
          <button
            type="button"
            onClick={() => setCollapsed(true)}
            title="收起会话列表"
            className="rounded-md p-1.5 text-text-3 transition-all hover:bg-surface-3 hover:text-text active:scale-95"
          >
            <ChevronLeftIcon />
          </button>
        </div>
      </div>

      <div className="flex-1 overflow-y-auto px-2.5 pb-3">
        {loading && sessions.length === 0 && (
          <div className="py-6 text-center text-[12px] text-text-3">加载会话...</div>
        )}

        {!loading && sessions.length === 0 && (
          <div className="flex flex-col items-start gap-2 rounded-lg border border-border/60 bg-surface-2 px-3 py-4 text-text-3">
            <p className="text-xs font-medium text-text-2">暂无会话</p>
            <p className="text-[11px] leading-relaxed">创建一个新会话后，历史记录会显示在这里。</p>
            <button
              type="button"
              onClick={onCreate}
              className="mt-1 text-xs font-medium text-primary hover:underline"
            >
              新建会话
            </button>
          </div>
        )}

        <div className="flex flex-col gap-1">
          {sessions.map((s) => (
            <SessionItem
              key={s.id}
              session={s}
              active={s.id === currentId}
              onClick={() => onSwitch(s.id)}
              onDelete={() => onDelete(s.id)}
            />
          ))}
        </div>
      </div>
    </aside>
  )
}

function ChevronLeftIcon() {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
    >
      <path d="m15 18-6-6 6-6" />
    </svg>
  )
}

function ChevronRightIcon() {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
    >
      <path d="m9 18 6-6-6-6" />
    </svg>
  )
}
