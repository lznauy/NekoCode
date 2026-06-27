import { useCallback, useEffect, useRef } from 'react'
import type { DependencyList } from 'react'

export interface UseAutoScrollReturn {
  containerRef: React.RefObject<HTMLDivElement>
  endRef: React.RefObject<HTMLDivElement>
  follow: () => void
}

// 自动滚动到最新内容：
// - 发消息/新回复到达时，若用户接近底部则跟随。
// - 用户手动向上翻阅时暂停跟随，回到接近底部时自动恢复。
// - 通过 scroll 事件区分用户意图: 程序滚动直接设置容器 scrollTop，同时
//   标记 isProgrammaticScroll 让下一次 scroll 事件跳过意图判定。
// - 使用双 rAF 保证 @tanstack/react-virtual 测量完成后再落位。
export function useAutoScroll(deps: DependencyList): UseAutoScrollReturn {
  const containerRef = useRef<HTMLDivElement>(null!)
  const endRef = useRef<HTMLDivElement>(null!)
  const rafRef = useRef<number | null>(null)
  const isProgrammatic = useRef(false)
  const shouldFollowRef = useRef(true)

  const scrollToBottom = useCallback(() => {
    const el = containerRef.current
    if (!el) return
    isProgrammatic.current = true
    // 双 rAF 已等虚拟机测量完成，此处 scrollHeight 已反映真实内容高度。
    el.scrollTop = el.scrollHeight
  }, [])

  const follow = useCallback(() => {
    shouldFollowRef.current = true
    if (rafRef.current !== null) return
    rafRef.current = requestAnimationFrame(() => {
      // 第二帧等虚拟机测量落定
      rafRef.current = requestAnimationFrame(() => {
        rafRef.current = null
        scrollToBottom()
      })
    })
  }, [scrollToBottom])

  useEffect(() => {
    const el = containerRef.current
    if (!el) return

    const handleScroll = () => {
      if (isProgrammatic.current) {
        isProgrammatic.current = false
        return
      }
      const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 100
      shouldFollowRef.current = nearBottom
    }

    el.addEventListener('scroll', handleScroll, { passive: true })
    return () => el.removeEventListener('scroll', handleScroll)
  }, [])

  useEffect(() => {
    if (!shouldFollowRef.current) return
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
