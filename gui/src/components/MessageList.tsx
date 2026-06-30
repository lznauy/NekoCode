import { createContext, forwardRef, memo, useContext, useLayoutEffect, useMemo, useRef } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import type { Msg } from '../types/events'
import { EmptyState } from './EmptyState'
import { MessageItem } from './MessageItem'

const ScrollContext = createContext<React.RefObject<HTMLDivElement | null>>({ current: null })
export const useScrollContainer = () => useContext(ScrollContext)

interface MessageListProps {
  msgs: Msg[]
  endRef: React.RefObject<HTMLDivElement>
  onPromptSelect?: (prompt: string) => void
  toggleStep: (stepId: string) => void
}

const VIRTUALIZE_AFTER = 80
const ITEM_GAP = 16

// 静态长历史才虚拟化。流式 RunCard 高度会持续变化, 如果同时动态 measure
// virtual item, scrollHeight 与 scrollTop 会被连续校正, 滚动条会产生肉眼可见的抖动。
export const MessageList = memo(forwardRef<HTMLDivElement, MessageListProps>(
  function MessageList({ msgs, endRef, onPromptSelect, toggleStep }, ref) {
    const scrollRef = useRef<HTMLDivElement>(null)
    const hasStreaming = useMemo(() => msgs.some((m) => m.streaming), [msgs])
    const shouldVirtualize = msgs.length > VIRTUALIZE_AFTER && !hasStreaming

    useLayoutEffect(() => {
      if (typeof ref === 'function') ref(scrollRef.current)
      else if (ref) (ref as React.MutableRefObject<HTMLDivElement | null>).current = scrollRef.current
    })

    const virtualizer = useVirtualizer({
      count: msgs.length,
      getScrollElement: () => scrollRef.current,
      estimateSize: () => 180,
      overscan: 5,
      measureElement: (el) => {
        const h = el.getBoundingClientRect().height
        return Math.ceil(h) + ITEM_GAP
      },
    })

    return (
      <div
        ref={scrollRef}
        className="mx-auto w-full max-w-[980px] flex-1 overflow-y-auto overflow-anchor-none px-5 py-6 scrollbar-gutter-stable"
        style={{ overflowAnchor: 'none' }}
      >
        {msgs.length === 0 ? (
          <EmptyState onPromptSelect={onPromptSelect} />
        ) : (
          <ScrollContext.Provider value={scrollRef}>
            {shouldVirtualize ? (
              <div
                data-testid="virtual-message-list"
                style={{ height: virtualizer.getTotalSize(), position: 'relative', width: '100%' }}
              >
                {virtualizer.getVirtualItems().map((vi) => {
                  const msg = msgs[vi.index]
                  return (
                    <div
                      key={msg.id}
                      data-index={vi.index}
                      ref={virtualizer.measureElement}
                      style={{
                        position: 'absolute',
                        top: 0,
                        left: 0,
                        width: '100%',
                        transform: `translateY(${vi.start}px)`,
                      }}
                    >
                      <MessageItem msg={msg} toggleStep={toggleStep} />
                    </div>
                  )
                })}
              </div>
            ) : (
              <div className="flex w-full flex-col gap-4">
                {msgs.map((msg) => (
                  <MessageItem key={msg.id} msg={msg} toggleStep={toggleStep} />
                ))}
              </div>
            )}
          </ScrollContext.Provider>
        )}
        <div ref={endRef} />
      </div>
    )
  },
))
