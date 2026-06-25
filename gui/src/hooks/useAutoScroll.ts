import { useCallback, useEffect, useRef } from 'react'
import type { DependencyList } from 'react'

export interface UseAutoScrollReturn {
  containerRef: React.RefObject<HTMLDivElement>
  endRef: React.RefObject<HTMLDivElement>
  isNearBottom: () => boolean
}

export function useAutoScroll(deps: DependencyList): UseAutoScrollReturn {
  const containerRef = useRef<HTMLDivElement>(null!)
  const endRef = useRef<HTMLDivElement>(null!)

  const isNearBottom = useCallback((): boolean => {
    const el = containerRef.current
    if (!el) return true
    return el.scrollHeight - el.scrollTop - el.clientHeight < 100
  }, [])

  useEffect(() => {
    if (isNearBottom()) {
      endRef.current?.scrollIntoView({ behavior: 'smooth' })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps)

  return { containerRef, endRef, isNearBottom }
}
