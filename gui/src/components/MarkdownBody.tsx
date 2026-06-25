import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { CodeBlock } from './CodeBlock'

interface MarkdownBodyProps {
  text: string
}

export function MarkdownBody({ text }: MarkdownBodyProps) {
  return (
    <div className="markdown">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
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
        }}
      >
        {text}
      </ReactMarkdown>
    </div>
  )
}
