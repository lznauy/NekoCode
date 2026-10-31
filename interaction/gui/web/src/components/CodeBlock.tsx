import { memo, useEffect, useRef, useState } from 'react'

interface CodeBlockProps {
  lang: string
  code: string
}

export const CodeBlock = memo(function CodeBlock({ lang, code }: CodeBlockProps) {
  const [copied, setCopied] = useState(false)

  const [copyError, setCopyError] = useState('')
  const timer = useRef<ReturnType<typeof setTimeout>>()
  const generation = useRef(0)
  useEffect(() => {
    generation.current++
    setCopied(false)
    setCopyError('')
    return () => { generation.current++; clearTimeout(timer.current) }
  }, [code])

  const handleCopy = async () => {
    const request = generation.current
    setCopyError('')
    setCopied(false)
    try {
      if (!navigator.clipboard) throw new Error('clipboard unavailable')
      await navigator.clipboard.writeText(code)
      if (request !== generation.current) return
      setCopied(true)
      clearTimeout(timer.current)
      timer.current = setTimeout(() => setCopied(false), 1500)
    } catch {
      if (request === generation.current) setCopyError('复制失败，请手动选择文本复制。')
    }
  }

  return (
    <div className="my-3 overflow-hidden card-radius bg-surface border border-border/50">
      <div className="flex h-7 items-center justify-between px-3 text-[10px] text-text-3">
        <span className="font-mono uppercase tracking-[0.12em]">{lang}</span>
        <button
          type="button"
          onClick={() => void handleCopy()}
          className="rounded px-1.5 py-0.5 text-[10px] transition-colors hover:bg-surface-3 hover:text-text-2"
        >
          {copied ? '已复制' : '复制'}
        </button>
      </div>
      {copyError && <p role="alert" className="px-3 py-1 text-xs text-danger">{copyError}</p>}
      <pre className="overflow-x-auto px-4 py-3 text-[12.5px] leading-[1.6]">
        <code className="font-mono">{code}</code>
      </pre>
    </div>
  )
})