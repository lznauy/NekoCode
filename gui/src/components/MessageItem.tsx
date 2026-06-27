import { memo } from 'react'
import type { Msg } from '../types/events'
import { MarkdownBody } from './MarkdownBody'
import { ImageGrid, RunCard } from './run'

interface MessageItemProps {
  msg: Msg
  toggleStep: (stepId: string) => void
}

// assistant 消息: 带有 Run 元数据 (steps/reasoning/todos/phase/tokens/...) 即渲染 RunCard;
// 否则降级为纯 Markdown (兼容历史消息或错误文本)。
function isRunMsg(m: Msg): boolean {
  return (
    m.role === 'assistant' &&
    (!!m.steps?.length || !!m.subagents?.length || !!m.todos?.length ||
     !!m.phase || m.reasoning !== undefined || m.tokens !== undefined)
  )
}

export const MessageItem = memo(function MessageItem({ msg, toggleStep }: MessageItemProps) {
  const isUser = msg.role === 'user'

  // 用户消息: 右对齐气泡。
  if (isUser) {
    return (
      <div className="flex justify-end">
        <div className="max-w-[78%] rounded-2xl rounded-br-md bg-primary/12 px-3.5 py-2 text-sm leading-relaxed text-text [overflow-wrap:break-word]">
          <MarkdownBody text={msg.text} />
        </div>
      </div>
    )
  }

  // 工具错误/旧式 tool 角色: 保留小卡片。
  if (msg.role === 'tool') {
    return (
      <div className="flex flex-col gap-1">
        <div className="ml-1 text-[10px] uppercase tracking-[0.18em] text-text-3">tool</div>
        <div className="rounded-lg bg-surface-2 px-3 py-2 text-sm text-text-2 [overflow-wrap:break-word]">
          <MarkdownBody text={msg.text} />
        </div>
      </div>
    )
  }

  // assistant: 直接是 RunCard (默认带顶栏); 无 Run 元数据时回退纯文本。
  if (isRunMsg(msg)) {
    return <RunCard msg={msg} toggleStep={toggleStep} />
  }

  // 兼容历史/错误文本: 纯内容流式, 不再围气泡。
  const hasImages = !!(msg.images?.length)
  const hasText = !!(msg.text || msg.streaming)
  return (
    <div className="flex flex-col gap-1">
      <div className="ml-1 text-[12px] text-text-2">{msg.streaming && <StreamGlyph />}</div>
      {hasText && (
        <div className="min-w-0 rounded-lg bg-surface-2/60 px-3.5 py-2.5 text-sm leading-relaxed text-text [overflow-wrap:break-word]">
          {msg.streaming ? <StreamText text={msg.text} /> : <MarkdownBody text={msg.text} />}
        </div>
      )}
      {hasImages && <ImageGrid images={msg.images!} />}
    </div>
  )
})

function StreamGlyph() {
  return <span className="text-primary">●</span>
}

function StreamText({ text }: { text: string }) {
  return <div className="whitespace-pre-wrap [overflow-wrap:break-word]">{text}</div>
}