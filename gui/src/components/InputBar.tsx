import { useCallback } from 'react'

interface InputBarProps {
  text: string
  busy: boolean
  model: string
  textareaRef: React.RefObject<HTMLTextAreaElement>
  onChange: (text: string) => void
  onSend: () => void
  onStop: () => void
  onTextareaChange: () => void
}

export function InputBar({
  text,
  busy,
  model,
  textareaRef,
  onChange,
  onSend,
  onStop,
  onTextareaChange,
}: InputBarProps) {
  const handleChange = useCallback(
    (e: React.ChangeEvent<HTMLTextAreaElement>) => {
      onChange(e.target.value)
      onTextareaChange()
    },
    [onChange, onTextareaChange],
  )

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault()
        onSend()
      }
    },
    [onSend],
  )

  return (
    <div className="border-t border-border/60 bg-surface-2 px-5 pb-5 pt-3">
      <div className="mx-auto flex w-full max-w-[980px] flex-col gap-2 rounded-xl border border-border/80 bg-surface p-2.5 surface-shadow transition-colors focus-within:border-primary/70">
        <textarea
          ref={textareaRef}
          value={text}
          onChange={handleChange}
          onKeyDown={handleKeyDown}
          disabled={busy}
          rows={1}
          placeholder={busy ? '正在处理...' : '输入任务，Enter 发送，Shift+Enter 换行'}
          className="mx-1 my-0.5 max-h-[180px] min-h-[22px] w-full resize-none bg-transparent text-sm leading-[1.5] text-text outline-none placeholder:text-text-3 disabled:opacity-40"
        />
        <div className="flex items-center gap-2 px-1">
          <span className="truncate text-[10px] leading-none text-text-3 tabular-nums">{model || '—'}</span>
          <span className="flex-1" />
          {busy ? (
            <button
              type="button"
              onClick={onStop}
              className="flex min-w-20 items-center justify-center gap-1.5 rounded-md bg-danger/90 px-3 py-1.5 text-[12.5px] font-medium leading-none text-white transition-all hover:bg-danger active:scale-95"
            >
              <span className="h-2.5 w-2.5 rounded-sm bg-white/90" /> 停止
            </button>
          ) : (
            <button
              type="button"
              onClick={onSend}
              disabled={!text.trim()}
              className="flex min-w-20 items-center justify-center gap-1.5 rounded-md bg-primary px-3 py-1.5 text-[12.5px] font-semibold leading-none text-black transition-all hover:brightness-110 active:scale-95 disabled:cursor-default disabled:opacity-25 disabled:active:scale-100"
            >
              发送 <span aria-hidden>↗</span>
            </button>
          )}
        </div>
      </div>
    </div>
  )
}
