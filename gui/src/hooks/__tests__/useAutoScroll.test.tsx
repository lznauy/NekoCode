// @vitest-environment jsdom

import { renderHook, act } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { useAutoScroll } from '../useAutoScroll'

describe('useAutoScroll', () => {
  it('follow() scrolls to bottom via double-rAF', () => {
    vi.useFakeTimers()
    const el = document.createElement('div')
    document.body.appendChild(el)
    Object.defineProperty(el, 'scrollHeight', { configurable: true, writable: true, value: 1000 })
    Object.defineProperty(el, 'clientHeight', { configurable: true, writable: true, value: 200 })

    const { result } = renderHook(() => useAutoScroll([0]))
    ;(result.current.containerRef as any).current = el

    act(() => {
      result.current.follow()
      vi.advanceTimersByTime(50)
    })
    // rAF1 ran → queued rAF2; need second advance
    act(() => { vi.advanceTimersByTime(50) })
    expect(el.scrollTop).toBe(800)
    vi.useRealTimers()
  })

  it('far from bottom: deps change does NOT scroll (user reading history)', () => {
    vi.useFakeTimers()
    const el = document.createElement('div')
    document.body.appendChild(el)
    Object.defineProperty(el, 'scrollHeight', { configurable: true, writable: true, value: 1000 })
    Object.defineProperty(el, 'clientHeight', { configurable: true, writable: true, value: 200 })
    el.scrollTop = 100 // far from bottom

    let dep = 0
    const { result, rerender } = renderHook(() => useAutoScroll([dep]))
    ;(result.current.containerRef as any).current = el

    // Change dep (simulate stream arrival) while user is up in history
    dep = 1
    rerender()
    act(() => {
      vi.advanceTimersByTime(50)
      vi.advanceTimersByTime(50)
    })
    expect(el.scrollTop).toBe(100) // NOT pulled back
    vi.useRealTimers()
  })

  it('near bottom: deps change scrolls to new bottom', () => {
    vi.useFakeTimers()
    const el = document.createElement('div')
    document.body.appendChild(el)
    Object.defineProperty(el, 'scrollHeight', { configurable: true, writable: true, value: 1000 })
    Object.defineProperty(el, 'clientHeight', { configurable: true, writable: true, value: 200 })
    el.scrollTop = 950 // near bottom (1000-950-200=-50 < 100)

    let dep = 0
    const { result, rerender } = renderHook(() => useAutoScroll([dep]))
    ;(result.current.containerRef as any).current = el

    // Stream grows content → user stays near new bottom
    Object.defineProperty(el, 'scrollHeight', { configurable: true, writable: true, value: 3000 })
    el.scrollTop = 2850 // still near bottom (3000-2850-200=-50 < 100)
    dep = 1
    rerender()
    act(() => {
      vi.advanceTimersByTime(50)
      vi.advanceTimersByTime(50)
    })
    expect(el.scrollTop).toBe(2800)
    vi.useRealTimers()
  })
})
