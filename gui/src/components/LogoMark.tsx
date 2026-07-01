import { cn } from '../lib/classnames'

interface LogoMarkProps {
  size?: 'sm' | 'md' | 'lg'
  showWordmark?: boolean
}

// NekoCode logo: rounded cat face with glowing AI eyes.
// viewBox is 32x32; designed to stay legible down to ~17px (TopBar usage).
export function LogoMark({ size = 'md', showWordmark = false }: LogoMarkProps) {
  const box = size === 'lg' ? 'h-12 w-12' : size === 'sm' ? 'h-7 w-7' : 'h-9 w-9'
  const icon = size === 'lg' ? 28 : size === 'sm' ? 17 : 22

  return (
    <span className="inline-flex items-center gap-2.5">
      <span
        className={cn(
          'relative inline-flex shrink-0 items-center justify-center overflow-hidden rounded-md bg-gradient-to-br from-[#7C5CFC] to-[#F472B6] text-black shadow-sm',
          box,
        )}
        aria-hidden
      >
        <svg width={icon} height={icon} viewBox="0 0 32 32" fill="none">
          {/* face */}
          <circle cx="16" cy="17" r="11" fill="#241F3D" />
          {/* ears */}
          <path d="M6 9 L9 3 L13 8 Z" fill="#241F3D" />
          <path d="M26 9 L23 3 L19 8 Z" fill="#241F3D" />
          <path d="M8 8 L9.5 5 L12 7.5 Z" fill="#F472B6" />
          <path d="M24 8 L22.5 5 L20 7.5 Z" fill="#F472B6" />
          {/* blush */}
          <ellipse cx="9" cy="19" rx="2.2" ry="1.2" fill="#F472B6" opacity="0.5" />
          <ellipse cx="23" cy="19" rx="2.2" ry="1.2" fill="#F472B6" opacity="0.5" />
          {/* eyes (glowing cyan) */}
          <ellipse cx="12" cy="15" rx="2.6" ry="3.2" fill="#0E0E1A" />
          <ellipse cx="12" cy="15" rx="1.5" ry="2.4" fill="#22D3EE" />
          <circle cx="11.4" cy="14" r="0.9" fill="#fff" />

          <ellipse cx="20" cy="15" rx="2.6" ry="3.2" fill="#0E0E1A" />
          <ellipse cx="20" cy="15" rx="1.5" ry="2.4" fill="#22D3EE" />
          <circle cx="19.4" cy="14" r="0.9" fill="#fff" />
          {/* nose */}
          <path d="M15 20 L17 20 L16 21.5 Z" fill="#FBCFE8" />
          {/* mouth */}
          <path d="M13.5 22.5 Q15 23.5 16 22.5 Q17 23.5 18.5 22.5" stroke="#FBCFE8" strokeWidth="1" fill="none" strokeLinecap="round" />
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
