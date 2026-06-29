import { createContext, forwardRef, memo, useContext, useLayoutEffect, useRef } from 'react'
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

// 虚拟化消息列表: 只挂载可视区及少量缓冲区的消息, 滚出视口即 unmount。
// RunCard 等大子树因此不常驻 DOM, 从根上消除"滚到大块卡顿"。
// 动态高度: measureElement 实测真实高度, 流式增长自动重排。
// 布局: 外层 scroll 容器沿用原 max-w-[980px] 居中 + padding; 内部 spacer
// (height = 总高度) 撑出可滚动总长, 每条消息用 absolute + translateY 定位,
// 与 gap-4 无关 (消息自身已有 margin/gap 兜底由 MessageItem 包裹结构决定)。
//
// overflow-anchor:none 关闭浏览器原生 Scroll Anchoring (流式时内容在下方持续
// 增高, 锚定会自动下滚 scrollTop), 把跟随决策权交还 useAutoScroll。
export const MessageList = memo(forwardRef<HTMLDivElement, MessageListProps>(
  function MessageList({ msgs, endRef, onPromptSelect, toggleStep }, ref) {
    const scrollRef = useRef<HTMLDivElement>(null)

    useLayoutEffect(() => {
      if (typeof ref === 'function') ref(scrollRef.current)
      else if (ref) (ref as React.MutableRefObject<HTMLDivElement | null>).current = scrollRef.current
    })

const virtualizer = useVirtualizer({
      count: msgs.length,
      getScrollElement: () => scrollRef.current,
      estimateSize: () => 200,
      overscan: 3,
      // 真实高度由 measureElement 测量, 否则估算高度与 RunCard 实际高度差距过大,
      // 会导致消息互相叠压或出现大段空白 (视觉错乱)。
      measureElement: (el) => {
        const h = el.getBoundingClientRect().height
        // +16px (gap-4) 作为消息之间的视觉间距, 替代原 flex 容器的 gap。
        return Math.ceil(h) + 16
      },
    })

    return (
      <div
        ref={scrollRef}
        className="mx-auto flex w-full max-w-[980px] flex-1 overflow-y-auto overflow-anchor-none px-5 py-6 scrollbar-gutter-stable"
        style={{ overflowAnchor: 'none' }}
      >
        {msgs.length === 0 ? (
          <EmptyState onPromptSelect={onPromptSelect} />
        ) : (
          <ScrollContext.Provider value={scrollRef}>
            <div
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
          </ScrollContext.Provider>
        )}
        <div ref={endRef} />
      </div>
    )
  },
))