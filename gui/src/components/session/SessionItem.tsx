import { useState } from 'react'
import { cn } from '../../lib/classnames'
import type { SessionMeta } from '../../types/session'

interface SessionItemProps {
  session: SessionMeta
  active: boolean
  onClick: () => void
  onDelete: () => void
}

export function SessionItem({ session, active, onClick, onDelete }: SessionItemProps) {
  const age = formatAge(session.updated_at)
  const [confirming, setConfirming] = useState(false)

  const handleDelete = (e: React.MouseEvent) => {
    e.stopPropagation()
    setConfirming(true)
  }

  const handleCancel = (e: React.MouseEvent) => {
    e.stopPropagation()
    setConfirming(false)
  }

  const handleConfirm = (e: React.MouseEvent) => {
    e.stopPropagation()
    setConfirming(false)
    onDelete()
  }

  if (confirming) {
    return (
      <div
        className="relative w-full rounded-lg border border-danger/35 bg-surface-3 px-3 py-2.5 text-left animate-slide-in"
        onClick={(e) => e.stopPropagation()}
      >
        <p className="text-[12px] text-text mb-2">
          删除 <span className="text-text font-medium">{session.id}</span>？
        </p>
        <p className="text-[10px] text-text-3 mb-3">操作不可撤销。</p>
        <div className="flex gap-2">
          <button
            type="button"
            onClick={handleConfirm}
            className="flex-1 rounded-md bg-danger/90 px-2.5 py-1.5 text-[12px] font-medium text-white transition-all hover:bg-danger active:scale-95"
          >
            确认删除
          </button>
          <button
            type="button"
            onClick={handleCancel}
            className="rounded-md border border-border px-3 py-1.5 text-[12px] text-text-2 transition-all hover:bg-surface-2 hover:text-text active:scale-95"
          >
            取消
          </button>
        </div>
      </div>
    )
  }

  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        'group relative w-full rounded-lg border px-3 py-2.5 text-left transition-all active:scale-[0.99]',
        'border-transparent hover:border-border/80 hover:bg-surface-2',
        active && 'bg-surface-3 border-primary/40',
      )}
    >
      <div className="flex items-center justify-between gap-2">
        <span className={cn('truncate text-[13px] font-medium', active ? 'text-primary' : 'text-text-2 group-hover:text-text')}>
          {session.id}
        </span>
        <button
          type="button"
          onClick={handleDelete}
          title="删除会话"
          className="shrink-0 rounded-md p-1 text-text-3 opacity-0 transition-all group-hover:opacity-100 hover:bg-danger/10 hover:text-danger active:scale-95"
        >
          <TrashIcon />
        </button>
      </div>
      <div className="truncate text-[11px] text-text-3">{session.cwd}</div>
      <div className="mt-1 flex items-center gap-1.5 text-[10px] text-text-3">
        <span>{session.msg_count} 条</span>
        <span className="opacity-40">·</span>
        <span>{age}</span>
      </div>
    </button>
  )
}

function TrashIcon() {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width="13"
      height="13"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
    >
      <path d="M3 6h18M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
    </svg>
  )
}

function formatAge(ts: number): string {
  const diff = Date.now() / 1000 - ts
  if (diff < 60) return '刚刚'
  if (diff < 3600) return `${Math.floor(diff / 60)} 分钟前`
  if (diff < 86400) return `${Math.floor(diff / 3600)} 小时前`
  return `${Math.floor(diff / 86400)} 天前`
}
