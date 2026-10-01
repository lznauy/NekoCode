import { cn } from '../lib/classnames'

interface LogoMarkProps {
  size?: 'sm' | 'md' | 'lg'
  showWordmark?: boolean
}

// NekoCode logo: compact cat-head outline.
// viewBox is 32x32; designed to stay legible down to ~17px (TopBar usage).
export function LogoMark({ size = 'md', showWordmark = false }: LogoMarkProps) {
  const box = size === 'lg' ? 'h-12 w-12' : size === 'sm' ? 'h-7 w-7' : 'h-9 w-9'
  const icon = size === 'lg' ? 28 : size === 'sm' ? 17 : 22

  return (
    <span className="inline-flex items-center gap-2.5">
      <span
        className={cn(
          'relative inline-flex shrink-0 items-center justify-center overflow-hidden rounded-md bg-[#111214] text-primary shadow-sm ring-1 ring-border/80',
          box,
        )}
        aria-hidden
      >
        <svg width={icon} height={icon} viewBox="0 0 32 32" fill="none">
          <path d="M16 8.8 12.4 5.6c-.4-.3-1 0-1 .5l.5 4.8A8.1 8.1 0 0 0 8 18c0 4.6 3.4 7.6 8 7.6s8-3 8-7.6c0-2.9-1.5-5.5-3.9-7.1l.5-4.8c0-.5-.6-.8-1-.5L16 8.8Z" fill="currentColor" />
          <rect x="13.3" y="16.5" width="1.7" height="2.9" rx="0.85" fill="#111214" />
          <rect x="17" y="16.5" width="1.7" height="2.9" rx="0.85" fill="#111214" />
          <rect x="14.5" y="20.9" width="3" height="1.2" rx="0.6" fill="#111214" />
        </svg>
      </span>
      {showWordmark && (
        <span className="hidden text-[13px] font-semibold leading-none text-text select-none sm:inline">
          Neko<span className="text-primary">Code</span>
        </span>
      )}
    </span>
  )
}
