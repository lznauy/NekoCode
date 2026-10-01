import { useCallback, useEffect, useState } from 'react'
import { cn } from '../lib/classnames'
import type { GUICommandMenu } from '../lib/wails'
import type { SkillView } from '../types/skills'
import type { ImageInputAttachment } from '../hooks/useChat'

interface InputBarProps {
  text: string
  busy: boolean
  skills?: SkillView[]
  selectedSkill?: string
  textareaRef: React.RefObject<HTMLTextAreaElement>
  onChange: (text: string) => void
  onSend: () => void
  onStop: () => void
  onTextareaChange: () => void
  onSelectSkill?: (name: string) => void
  onClearSkill?: () => void
  commandMenu?: GUICommandMenu | null
  onSelectCommand?: (item: GUICommandMenu['items'][number]) => void
  imageAttachments?: ImageInputAttachment[]
  onPasteImages?: (files: File[], start: number, end: number) => Promise<void>
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
  commandMenu,
  onSelectCommand,
  imageAttachments = [],
  onPasteImages,
}: InputBarProps) {
	const [commandIndex, setCommandIndex] = useState(0)
	const [menuDismissed, setMenuDismissed] = useState(false)
	const [pastingImages, setPastingImages] = useState(false)
	useEffect(() => {
	  setCommandIndex(0)
	  setMenuDismissed(false)
	}, [commandMenu])
  const handleChange = useCallback(
    (e: React.ChangeEvent<HTMLTextAreaElement>) => {
      onChange(e.target.value)
      onTextareaChange()
    },
    [onChange, onTextareaChange],
  )

  const replaceAtomicSelection = useCallback((start: number, end: number, inserted: string) => {
    const range = expandImageSelection(text, imageAttachments, start, end)
    if (range.start === start && range.end === end) return false
    const next = text.slice(0, range.start) + inserted + text.slice(range.end)
    onChange(next)
    const textarea = textareaRef.current
    requestAnimationFrame(() => {
      const cursor = range.start + inserted.length
      textarea?.setSelectionRange(cursor, cursor)
      onTextareaChange()
    })
    return true
  }, [imageAttachments, onChange, onTextareaChange, text, textareaRef])

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
	  const atomic = imageMarkerForKey(text, imageAttachments, e.currentTarget.selectionStart, e.currentTarget.selectionEnd, e.key)
	  if (atomic) {
		e.preventDefault()
		const textarea = e.currentTarget
		const next = text.slice(0, atomic.start) + text.slice(atomic.end)
		onChange(next)
		requestAnimationFrame(() => {
		  textarea.setSelectionRange(atomic.start, atomic.start)
		  onTextareaChange()
		})
		return
	  }
	  if (cursorInsideImageMarker(text, imageAttachments, e.currentTarget.selectionStart) && e.key.length === 1 && !e.ctrlKey && !e.metaKey) {
		e.preventDefault()
		return
	  }
	  const items = menuDismissed ? [] : (commandMenu?.items ?? [])
	  if (items.length > 0 && (e.key === 'ArrowDown' || e.key === 'ArrowUp')) {
		e.preventDefault()
		setCommandIndex((current) => (current + (e.key === 'ArrowDown' ? 1 : -1) + items.length) % items.length)
		return
	  }
	  if (commandMenu && e.key === 'Escape') {
		e.preventDefault()
		setMenuDismissed(true)
		return
	  }
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault()
		if (items.length > 0) onSelectCommand?.(items[commandIndex])
		else onSend()
      }
    },
	[commandIndex, commandMenu, imageAttachments, menuDismissed, onChange, onSelectCommand, onSend, onTextareaChange, text],
  )

  const handlePaste = useCallback((e: React.ClipboardEvent<HTMLTextAreaElement>) => {
    const files = Array.from(e.clipboardData.items)
      .filter((item) => item.kind === 'file' && item.type.startsWith('image/'))
      .map((item) => item.getAsFile())
      .filter((file): file is File => file !== null)
    const start = e.currentTarget.selectionStart
    const end = e.currentTarget.selectionEnd
	const range = expandImageSelection(text, imageAttachments, start, end)
	if (files.length === 0 || !onPasteImages) {
	  if (range.start === start && range.end === end) return
	  e.preventDefault()
	  replaceAtomicSelection(start, end, e.clipboardData.getData('text/plain'))
	  return
	}
    e.preventDefault()
    setPastingImages(true)
    void onPasteImages(files, range.start, range.end).finally(() => {
      setPastingImages(false)
      requestAnimationFrame(onTextareaChange)
    })
  }, [imageAttachments, onPasteImages, onTextareaChange, replaceAtomicSelection, text])

  const handleBeforeInput = useCallback((e: React.FormEvent<HTMLTextAreaElement>) => {
	const native = e.nativeEvent as InputEvent
	if (!native.inputType.startsWith('insert') && !native.inputType.startsWith('delete')) return
	const start = e.currentTarget.selectionStart
	const end = e.currentTarget.selectionEnd
	const inserted = native.inputType === 'insertText' || native.inputType === 'insertCompositionText' ? (native.data ?? '') : ''
	if (replaceAtomicSelection(start, end, inserted)) e.preventDefault()
  }, [replaceAtomicSelection])

  return (
    <div className="border-t border-border/50 bg-surface-2 px-5 pb-5 pt-3">
      <div className="mx-auto flex w-full max-w-[980px] flex-col gap-2 card-radius border border-border/60 bg-surface p-2.5 transition-colors focus-within:border-primary/70">
		{commandMenu && !menuDismissed && (
		  <div className="mb-1 overflow-hidden rounded-lg border border-border/60 bg-surface-2/80">
			<div className="flex items-center justify-between border-b border-border/50 px-3 py-2">
			  <span className="text-[11px] font-semibold tracking-wide text-text-2">{commandMenu.title || 'Commands'}</span>
			  <span className="text-[10px] text-text-3">↑↓ 选择 · Enter 确认 · Esc 关闭</span>
			</div>
			<div className="max-h-60 overflow-y-auto p-1.5">
			  {commandMenu.items.length === 0 ? (
				<div className="px-2 py-4 text-center text-[11px] text-text-3">{commandMenu.empty || '暂无可选项'}</div>
			  ) : commandMenu.items.map((item, index) => (
				<button
				  key={`${item.value}-${index}`}
				  type="button"
				  onMouseEnter={() => setCommandIndex(index)}
				  onClick={() => onSelectCommand?.(item)}
				  className={cn(
					'flex w-full items-start gap-3 rounded-md px-2.5 py-2 text-left transition-colors',
					index === commandIndex ? 'bg-primary/12 text-text' : 'text-text-2 hover:bg-surface-3',
				  )}
				>
				  <span className="min-w-36 font-mono text-[12px] font-medium text-primary">{item.label || item.value}</span>
				  {item.description && <span className="line-clamp-2 text-[11px] leading-4 text-text-3">{item.description}</span>}
				</button>
			  ))}
			</div>
		  </div>
		)}
        <textarea
          ref={textareaRef}
          value={text}
          onChange={handleChange}
          onKeyDown={handleKeyDown}
          onBeforeInput={handleBeforeInput}
          onPaste={handlePaste}
          disabled={busy || pastingImages}
          rows={1}
          placeholder={busy ? '正在处理...' : pastingImages ? '正在附加图片...' : '描述要修改、排查或构建的内容'}
          className="mx-1 my-0.5 max-h-[180px] min-h-[24px] w-full resize-none bg-transparent text-sm leading-[1.5] text-text outline-none placeholder:text-text-3 disabled:opacity-40"
        />
        <div className="flex min-h-[30px] items-center gap-2 px-1">
          <div className="group relative flex h-[30px] items-center">
            <button
              type="button"
              className={cn(
                'inline-flex h-7 max-w-[220px] items-center rounded-md px-2.5 text-[11px] font-medium leading-none transition-all active:scale-95',
                selectedSkill ? 'bg-primary/15 text-primary' : 'bg-surface-2 text-text-3 hover:bg-surface-3 hover:text-text',
              )}
            >
              <span className="truncate">{selectedSkill ? `Skill: ${selectedSkill}` : 'Skill'}</span>
            </button>
            <div className="invisible absolute bottom-full left-0 z-50 mb-1 max-h-72 w-72 overflow-y-auto pill-radius border border-border/70 bg-surface p-1 opacity-0 surface-shadow transition-all group-focus-within:visible group-focus-within:opacity-100 group-hover:visible group-hover:opacity-100">
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
          <span className="hidden h-7 items-center text-[10.5px] leading-none text-text-3 sm:inline-flex">
            {pastingImages ? '正在附加图片…' : 'Enter 发送 · Shift+Enter 换行 · 可粘贴图片'}
          </span>
          <span className="flex-1" />
          {busy ? (
            <button
              type="button"
              onClick={onStop}
              className="flex h-7 min-w-20 items-center justify-center gap-1.5 rounded-md bg-danger/90 px-3 text-[12.5px] font-medium leading-none text-white transition-all hover:bg-danger active:scale-95"
            >
              <span className="h-2.5 w-2.5 rounded-sm bg-white/90" /> 停止
            </button>
          ) : (
            <button
              type="button"
              onClick={onSend}
              disabled={!text.trim() || pastingImages}
              className="flex h-7 min-w-20 items-center justify-center gap-1.5 rounded-md bg-primary px-3 text-[12.5px] font-semibold leading-none text-black transition-all hover:brightness-110 active:scale-95 disabled:cursor-default disabled:opacity-25 disabled:active:scale-100"
            >
              发送 <SendIcon />
            </button>
          )}
        </div>
      </div>
    </div>
  )
}

function imageMarkerForKey(text: string, images: ImageInputAttachment[], selectionStart: number, selectionEnd: number, key: string) {
  if (key !== 'Backspace' && key !== 'Delete') return null
  for (const image of images) {
    const start = text.indexOf(image.label)
    if (start < 0) continue
    const end = start + image.label.length
    const selected = selectionStart < end && selectionEnd > start
    const backward = selectionStart === selectionEnd && key === 'Backspace' && selectionStart > start && selectionStart <= end
    const forward = selectionStart === selectionEnd && key === 'Delete' && selectionStart >= start && selectionStart < end
    if (selected || backward || forward) return { start, end }
  }
  return null
}

function cursorInsideImageMarker(text: string, images: ImageInputAttachment[], cursor: number): boolean {
  return images.some((image) => {
    const start = text.indexOf(image.label)
    return start >= 0 && cursor > start && cursor < start + image.label.length
  })
}

function expandImageSelection(text: string, images: ImageInputAttachment[], selectionStart: number, selectionEnd: number) {
  let start = selectionStart
  let end = selectionEnd
  for (const image of images) {
    const markerStart = text.indexOf(image.label)
    if (markerStart < 0) continue
    const markerEnd = markerStart + image.label.length
    const overlaps = start < markerEnd && end > markerStart
    const inside = start === end && start > markerStart && start < markerEnd
    if (overlaps || inside) {
      start = Math.min(start, markerStart)
      end = Math.max(end, markerEnd)
    }
  }
  return { start, end }
}

function SendIcon() {
  return (
    <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" aria-hidden>
      <path d="m22 2-7 20-4-9-9-4Z" />
      <path d="M22 2 11 13" />
    </svg>
  )
}
