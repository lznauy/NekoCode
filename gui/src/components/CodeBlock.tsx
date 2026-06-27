import { memo, useState } from 'react'

interface CodeBlockProps {
  lang: string
  code: string
}

export const CodeBlock = memo(function CodeBlock({ lang, code }: CodeBlockProps) {
  const [copied, setCopied] = useState(false)

  const handleCopy = () => {
    navigator.clipboard?.writeText(code).catch(() => {})
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }

  return (
    <div className="my-3 overflow-hidden rounded-xl bg-surface border border-border/50">
      <div className="flex h-7 items-center justify-between px-3 text-[10px] text-text-3">
        <span className="font-mono uppercase tracking-[0.12em]">{lang}</span>
        <button
          type="button"
          onClick={handleCopy}
          tabIndex={-1}
          className="rounded px-1.5 py-0.5 text-[10px] transition-colors hover:bg-surface-3 hover:text-text-2"
        >
          {copied ? '已复制' : '复制'}
        </button>
      </div>
      <pre className="overflow-x-auto px-4 py-3 text-[12.5px] leading-[1.6]">
        <code className="font-mono">{code}</code>
      </pre>
    </div>
  )
})