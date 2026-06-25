import { forwardRef } from 'react'
import type { Msg } from '../types/events'
import { EmptyState } from './EmptyState'
import { MessageItem } from './MessageItem'

interface MessageListProps {
  msgs: Msg[]
  endRef: React.RefObject<HTMLDivElement>
}

export const MessageList = forwardRef<HTMLDivElement, MessageListProps>(
  function MessageList({ msgs, endRef }, ref) {
    return (
      <div
        ref={ref}
        className="mx-auto flex w-full max-w-[980px] flex-1 flex-col gap-4 overflow-y-auto px-5 py-6 scrollbar-gutter-stable"
      >
        {msgs.length === 0 && <EmptyState />}
        {msgs.map((msg) => (
          <MessageItem key={msg.id} msg={msg} />
        ))}
        <div ref={endRef} />
      </div>
    )
  },
)
