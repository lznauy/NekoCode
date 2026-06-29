import { useCallback } from 'react'
import { cn } from '../lib/classnames'
import type { SkillSnapshot } from '../types/skills'

interface InputBarProps {
  text: string
  busy: boolean
  skills?: SkillSnapshot[]
  selectedSkill?: string
  textareaRef: React.RefObject<HTMLTextAreaElement>
  onChange: (text: string) => void
  onSend: () => void
  onStop: () => void
  onTextareaChange: () => void
  onSelectSkill?: (name: string) => void
  onClearSkill?: () => void
}

export function InputBar({
  text,
  busy,
  skills = [],
  selectedSkill,
  textareaRef,
  onChange,
  onSend,
  onStop,
  onTextareaChange,
  onSelectSkill,
  onClearSkill,
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
    <div className="border-t border-border/50 bg-surface-2 px-5 pb-5 pt-3">
      <div className="mx-auto flex w-full max-w-[980px] flex-col gap-2 rounded-lg border border-border/60 bg-surface p-2.5 transition-colors focus-within:border-primary/70">
        <textarea
          ref={textareaRef}
          value={text}
          onChange={handleChange}
          onKeyDown={handleKeyDown}
          disabled={busy}
          rows={1}
          placeholder={busy ? '正在处理...' : '描述要修改、排查或构建的内容'}
          className="mx-1 my-0.5 max-h-[180px] min-h-[24px] w-full resize-none bg-transparent text-sm leading-[1.5] text-text outline-none placeholder:text-text-3 disabled:opacity-40"
        />
        <div className="flex items-center gap-2 px-1">
          <div className="group relative">
            <button
              type="button"
              className={cn(
                'rounded-md px-2 py-1 text-[11px] leading-none transition-all active:scale-95',
                selectedSkill ? 'bg-primary/15 text-primary' : 'bg-surface-2 text-text-3 hover:bg-surface-3 hover:text-text',
              )}
            >
              {selectedSkill ? `Skill: ${selectedSkill}` : 'Skill'}
            </button>
            <div className="invisible absolute bottom-full left-0 z-50 mb-1 max-h-72 w-72 overflow-y-auto rounded-md border border-border/70 bg-surface p-1 opacity-0 surface-shadow transition-all group-focus-within:visible group-focus-within:opacity-100 group-hover:visible group-hover:opacity-100">
              {selectedSkill && (
                <button
                  type="button"
                  onClick={onClearSkill}
                  className="mb-1 block w-full rounded px-2 py-1.5 text-left text-[11px] text-danger hover:bg-danger/10"
                >
                  清除当前 Skill
                </button>
              )}
              {skills.map((skill) => (
                <button
                  key={skill.name}
                  type="button"
                  onClick={() => onSelectSkill?.(skill.name)}
                  className="block w-full rounded px-2 py-1.5 text-left hover:bg-surface-3"
                >
                  <span className="block truncate text-[12px] font-medium text-text">{skill.name}</span>
                  <span className="line-clamp-1 text-[10px] text-text-3">{skill.description || skill.source}</span>
                </button>
              ))}
              {skills.length === 0 && (
                <div className="px-2 py-3 text-center text-[11px] text-text-3">暂无可用 skill</div>
              )}
            </div>
          </div>
          <span className="hidden text-[10px] leading-none text-text-3 sm:inline">Enter 发送 · Shift+Enter 换行</span>
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
              发送 <SendIcon />
            </button>
          )}
        </div>
      </div>
    </div>
  )
}

function SendIcon() {
  return (
    <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" aria-hidden>
      <path d="m22 2-7 20-4-9-9-4Z" />
      <path d="M22 2 11 13" />
    </svg>
  )
}
