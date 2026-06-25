import { cn } from '../lib/classnames'
import { ThemeToggle } from './ThemeToggle'
import type { Theme } from '../hooks/useTheme'

interface TopBarProps {
  model: string
  busy: boolean
  theme: Theme
  onToggleTheme: () => void
  onClose: () => void
}

export function TopBar({ model, busy, theme, onToggleTheme, onClose }: TopBarProps) {
  return (
    <header className="flex items-center gap-2 border-b border-border/60 bg-surface-2/95 px-5">
      <span className="flex h-6 w-6 items-center justify-center rounded-md bg-primary text-[11px] font-bold leading-none text-black select-none">
        N
      </span>
      <span className="text-[13px] font-semibold leading-none text-text select-none">
        Neko<span className="text-primary">Code</span>
      </span>
      {model && (
        <span className="ml-2 max-w-[40vw] truncate rounded-md border border-border/60 bg-surface px-2 py-0.5 text-[10px] leading-none text-text-2">
          {model}
        </span>
      )}
      <span className="flex-1" />
      <ThemeToggle theme={theme} onToggle={onToggleTheme} />
      <span
        className={cn(
          'h-1.5 w-1.5 rounded-full transition-all duration-300',
          busy ? 'bg-primary animate-pulse-soft' : 'bg-success',
        )}
      />
      <span className="text-[11px] leading-none text-text-2 tabular-nums">
        {busy ? '思考中' : '就绪'}
      </span>
      <button
        type="button"
        onClick={onClose}
        title="关闭"
        aria-label="关闭窗口"
        className="ml-1 flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-text-3 transition-all hover:bg-danger/15 hover:text-danger active:scale-95"
      >
        <CloseIcon />
      </button>
    </header>
  )
}

function CloseIcon() {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width="15"
      height="15"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2.2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
    >
      <path d="M18 6 6 18" />
      <path d="m6 6 12 12" />
    </svg>
  )
}
