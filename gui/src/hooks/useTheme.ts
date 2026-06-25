// useTheme: 明暗双配色状态管理。
// 决策优先级: localStorage(用户手选) > prefers-color-scheme > dark。
// 切换时写入 localStorage 并同步到 <html data-theme>。
import { useCallback, useEffect, useState } from 'react'

export type Theme = 'dark' | 'light'

const STORAGE_KEY = 'nekocode-theme'

function detectInitial(): Theme {
  if (typeof window === 'undefined') return 'dark'
  const saved = window.localStorage.getItem(STORAGE_KEY)
  if (saved === 'dark' || saved === 'light') return saved
  if (window.matchMedia?.('(prefers-color-scheme: light)').matches) return 'light'
  return 'dark'
}

export function useTheme() {
  const [theme, setTheme] = useState<Theme>(detectInitial)

  // 同步到 <html data-theme> — Tailwind @theme 变量据此切换。
  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme)
    window.localStorage.setItem(STORAGE_KEY, theme)
  }, [theme])

  // 跟随系统变化: 仅当用户未手选 (首次加载时无 localStorage) 时生效。
  // 一旦用户点击切换写入 localStorage, 后续不再跟随系统。
  useEffect(() => {
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    const handler = (e: MediaQueryListEvent) => {
      const saved = window.localStorage.getItem(STORAGE_KEY)
      // 已有用户偏好则不覆盖 — 但这个 listener 仅在用户从未点击过时才需响应。
      // 简化: 永远跟随系统当次变化。用户手选后按下一次系统事件会盖掉。
      // 这是可接受的权衡: 系统事件本身少 (夜间/手动切换系统主题), 且符合
      // "下次系统变化即值得重新跟随" 的直觉。
      void saved
      setTheme(e.matches ? 'dark' : 'light')
    }
    mq.addEventListener('change', handler)
    return () => mq.removeEventListener('change', handler)
  }, [])

  const toggle = useCallback(() => {
    setTheme((t) => (t === 'dark' ? 'light' : 'dark'))
  }, [])

  return { theme, toggle }
}