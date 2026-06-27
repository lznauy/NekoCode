import { cn } from '../lib/classnames'

interface LogoMarkProps {
  size?: 'sm' | 'md' | 'lg'
  showWordmark?: boolean
}

export function LogoMark({ size = 'md', showWordmark = false }: LogoMarkProps) {
  const box = size === 'lg' ? 'h-12 w-12' : size === 'sm' ? 'h-7 w-7' : 'h-9 w-9'
  const icon = size === 'lg' ? 28 : size === 'sm' ? 17 : 22

  return (
    <span className="inline-flex items-center gap-2.5">
      <span
        className={cn(
          'relative inline-flex shrink-0 items-center justify-center overflow-hidden rounded-md bg-primary text-black shadow-sm',
          box,
        )}
        aria-hidden
      >
        <svg width={icon} height={icon} viewBox="0 0 32 32" fill="none">
          <path d="M8 22V10l8 12 8-12v12" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" />
          <path d="M9 8h4M19 8h4" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" />
          <path d="M11 25h10" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" opacity=".72" />
        </svg>
      </span>
      {showWordmark && (
        <span className="text-[13px] font-semibold leading-none text-text select-none">
          Neko<span className="text-primary">Code</span>
        </span>
      )}
    </span>
  )
}
