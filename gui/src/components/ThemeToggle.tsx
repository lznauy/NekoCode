// ThemeToggle: 顶栏明暗配色小开关。
// 月/日图标互转, 点击瞬时切换, 圆角按钮 + 悬停底色反馈。
// 图标用 stroke 矢量符号 (☾ / ☀) 直接用 Unicode 字符, 无需图标库依赖。
import type { Theme } from '../hooks/useTheme'

interface ThemeToggleProps {
  theme: Theme
  onToggle: () => void
}

export function ThemeToggle({ theme, onToggle }: ThemeToggleProps) {
  const isDark = theme === 'dark'
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-label={isDark ? '切换到明亮配色' : '切换到暗色配色'}
      title={isDark ? '明亮' : '暗色'}
      className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-[14px] leading-none text-text-2 transition-all hover:bg-surface-3/70 hover:text-text active:scale-95"
    >
      <span className="transition-transform duration-300" style={{ display: 'inline-block' }}>
        {isDark ? '☾' : '☀'}
      </span>
    </button>
  )
}
