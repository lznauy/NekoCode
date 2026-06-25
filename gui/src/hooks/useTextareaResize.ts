import { useCallback, useRef } from 'react'

export interface UseTextareaResizeReturn {
  taRef: React.RefObject<HTMLTextAreaElement>
  resize: () => void
}

export function useTextareaResize(maxHeight = 180): UseTextareaResizeReturn {
  const taRef = useRef<HTMLTextAreaElement>(null!)

  const resize = useCallback(() => {
    const el = taRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${Math.min(el.scrollHeight, maxHeight)}px`
  }, [maxHeight])

  return { taRef, resize }
}
