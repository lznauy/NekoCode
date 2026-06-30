import { cn } from '../lib/classnames'
import { LogoMark } from './LogoMark'
import { ThemeToggle } from './ThemeToggle'
import type { Theme } from '../hooks/useTheme'
import type { ModelConfig } from '../types/config'

interface TopBarProps {
  model: string
  models?: ModelConfig[]
  busy: boolean
  theme: Theme
  onToggleTheme: () => void
  onSwitchModel?: (name: string) => void
  onOpenContext?: () => void
  onOpenConfig: () => void
  onOpenSkills: () => void
  onClose: () => void
}

export function TopBar({ model, models = [], busy, theme, onToggleTheme, onSwitchModel, onOpenContext, onOpenConfig, onOpenSkills, onClose }: TopBarProps) {
  return (
    <header className="flex items-center gap-2 border-b border-border/60 bg-surface-2/95 px-5">
      <LogoMark size="sm" showWordmark />
      {model && (
        <div className="group relative ml-2">
          <button
            type="button"
            className="block max-w-[40vw] truncate rounded-md border border-border/60 bg-surface px-2 py-1 text-[10px] leading-none text-text-2 transition-all hover:border-primary/60 hover:text-text active:scale-95"
          >
            {model}
          </button>
          {models.length > 0 && (
            <div className="invisible absolute left-0 top-full z-50 mt-1 w-56 rounded-md border border-border/70 bg-surface p-1 opacity-0 surface-shadow transition-all group-focus-within:visible group-focus-within:opacity-100 group-hover:visible group-hover:opacity-100">
              {models.map((m) => (
                <button
                  key={m.name}
                  type="button"
                  onClick={() => onSwitchModel?.(m.name)}
                  className="block w-full rounded px-2 py-1.5 text-left text-[11px] text-text-2 hover:bg-surface-3 hover:text-text"
                >
                  <span className="block truncate font-medium">{m.name}</span>
                  <span className="block truncate font-mono text-[10px] text-text-3">{m.provider} / {m.model}</span>
                </button>
              ))}
            </div>
          )}
        </div>
      )}
      <span className="flex-1" />
      <button
        type="button"
        onClick={onOpenContext}
        title="上下文状态"
        aria-label="上下文状态"
        className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-text-3 transition-all hover:bg-surface-3 hover:text-text active:scale-95"
      >
        <ContextIcon />
      </button>
      <button
        type="button"
        onClick={onOpenSkills}
        title="技能管理"
        aria-label="技能管理"
        className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-text-3 transition-all hover:bg-surface-3 hover:text-text active:scale-95"
      >
        <SparkIcon />
      </button>
      <button
        type="button"
        onClick={onOpenConfig}
        title="配置管理"
        aria-label="配置管理"
        className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-text-3 transition-all hover:bg-surface-3 hover:text-text active:scale-95"
      >
        <GearIcon />
      </button>
      <ThemeToggle theme={theme} onToggle={onToggleTheme} />
      <span
        className={cn(
          'inline-flex h-7 items-center gap-1.5 rounded-md px-2 text-[11px] leading-none transition-colors',
          busy ? 'bg-primary/12 text-primary' : 'bg-success/12 text-success',
        )}
      >
        {busy ? <SpinnerIcon /> : <CheckIcon />}
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

function ContextIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.1" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <path d="M4 6h16" />
      <path d="M4 12h10" />
      <path d="M4 18h16" />
      <path d="M18 10v4" />
      <path d="M20 12h-4" />
    </svg>
  )
}

function SparkIcon() {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width="15"
      height="15"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2.1"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
    >
      <path d="M12 3v4" />
      <path d="M12 17v4" />
      <path d="M3 12h4" />
      <path d="M17 12h4" />
      <path d="m5.6 5.6 2.8 2.8" />
      <path d="m15.6 15.6 2.8 2.8" />
      <path d="m18.4 5.6-2.8 2.8" />
      <path d="m8.4 15.6-2.8 2.8" />
      <path d="M12 8.5 13.2 11l2.3 1-2.3 1L12 15.5 10.8 13l-2.3-1 2.3-1L12 8.5Z" />
    </svg>
  )
}

function SpinnerIcon() {
  return (
    <svg className="animate-spin" width="12" height="12" viewBox="0 0 24 24" fill="none" aria-hidden>
      <path d="M12 3a9 9 0 1 1-6.4 2.7" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" />
    </svg>
  )
}

function CheckIcon() {
  return (
    <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" aria-hidden>
      <path d="m5 12 4 4L19 6" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

function GearIcon() {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width="15"
      height="15"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2.1"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
    >
      <path d="M12 15.5a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7Z" />
      <path d="M19.4 15a1.8 1.8 0 0 0 .36 1.98l.04.04a2 2 0 0 1-2.82 2.82l-.04-.04a1.8 1.8 0 0 0-1.98-.36 1.8 1.8 0 0 0-1.1 1.66V21a2 2 0 0 1-4 0v-.06A1.8 1.8 0 0 0 8.8 19.3a1.8 1.8 0 0 0-1.98.36l-.04.04a2 2 0 1 1-2.82-2.82l.04-.04A1.8 1.8 0 0 0 4.36 15a1.8 1.8 0 0 0-1.66-1.1H2.6a2 2 0 0 1 0-4h.06A1.8 1.8 0 0 0 4.3 8.8a1.8 1.8 0 0 0-.36-1.98l-.04-.04a2 2 0 1 1 2.82-2.82l.04.04A1.8 1.8 0 0 0 8.8 4.36a1.8 1.8 0 0 0 1.1-1.66V2.6a2 2 0 0 1 4 0v.06a1.8 1.8 0 0 0 1.1 1.64 1.8 1.8 0 0 0 1.98-.36l.04-.04a2 2 0 1 1 2.82 2.82l-.04.04a1.8 1.8 0 0 0-.36 1.98 1.8 1.8 0 0 0 1.66 1.1h.1a2 2 0 0 1 0 4h-.06A1.8 1.8 0 0 0 19.4 15Z" />
    </svg>
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
