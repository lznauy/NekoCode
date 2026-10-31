import ReactMarkdown from 'react-markdown'
import type { Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { CodeBlock } from './CodeBlock'
import { memo } from 'react'

interface MarkdownBodyProps {
  text: string
}

// 组件映射保持稳定: 提升到模块作用域, 避免每次渲染重建对象导致内部元素 memo 失效。
const markdownComponents: Components = {
  img({ src, alt }: any) {
    const text = src ? (alt ? `${alt}: ${src}` : src) : alt || ''
    return (
      <code className="rounded bg-surface-3/70 px-[5px] py-px font-mono text-[0.85em] text-warning [overflow-wrap:anywhere]">
        {text}
      </code>
    )
  },
  code({ className, children, ...props }: any) {
    const cls = (className as string) || ''
    const isInline = !cls.startsWith('language-')
    if (isInline) {
      return (
        <code
          className="rounded bg-surface-3/70 px-[5px] py-px font-mono text-[0.85em] text-warning"
          {...props}
        >
          {children as React.ReactNode}
        </code>
      )
    }
    const lang = cls.replace('language-', '') || 'text'
    const code = String(children).replace(/\n$/, '')
    return <CodeBlock lang={lang} code={code} />
  },
}

const remarkPlugins = [remarkGfm]

export const MarkdownBody = memo(function MarkdownBody({ text }: MarkdownBodyProps) {
  return (
    <div className="markdown">
      <ReactMarkdown remarkPlugins={remarkPlugins} components={markdownComponents}>
        {text}
      </ReactMarkdown>
    </div>
  )
})
