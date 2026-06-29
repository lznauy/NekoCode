import { useCallback, useEffect, useRef } from 'react'
import type { DependencyList } from 'react'

export interface UseAutoScrollReturn {
  containerRef: React.RefObject<HTMLDivElement>
  endRef: React.RefObject<HTMLDivElement>
  follow: () => void
}

// 自动滚动到最新内容：
// - 每次 deps 变化（流式增长 / 新消息），仅当用户当前在底部附近（<100px）才滚
// - 用户向上滚 → 不在底部 → 下次 deps 变化自然不跟
// - follow() 在 send 时强制跟随（独立的双 rAF，不经过 deps effect）
// - 不追踪状态、不区分用户/程序滚动——只问一句话："现在在底部吗？"
export function useAutoScroll(deps: DependencyList): UseAutoScrollReturn {
  const containerRef = useRef<HTMLDivElement>(null!)
  const endRef = useRef<HTMLDivElement>(null!)
  const rafRef = useRef<number | null>(null)

  const scrollToBottom = useCallback(() => {
    const el = containerRef.current
    if (!el) return
    el.scrollTop = el.scrollHeight
  }, [])

  // send 时强制滚动，独立于 deps effect 的 nearBottom 判断
  const follow = useCallback(() => {
    if (rafRef.current !== null) return
    rafRef.current = requestAnimationFrame(() => {
      rafRef.current = requestAnimationFrame(() => {
        rafRef.current = null
        scrollToBottom()
      })
    })
  }, [scrollToBottom])

  // 每次 deps 变化：仅当用户**此刻**在底部附近时才滚到底部
  useEffect(() => {
    const el = containerRef.current
    if (!el) return

    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 100
    if (!nearBottom) return

    if (rafRef.current !== null) return
    rafRef.current = requestAnimationFrame(() => {
      rafRef.current = requestAnimationFrame(() => {
        rafRef.current = null
        scrollToBottom()
      })
    })
    return () => {
      if (rafRef.current !== null) {
        cancelAnimationFrame(rafRef.current)
        rafRef.current = null
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps)

  return { containerRef, endRef, follow }
}
