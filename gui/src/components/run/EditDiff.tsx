// EditDiff: edit 工具的 diff 预览。
// 头: 一行可点的折叠头 (折叠时无独立 border);
// 展开: 表格化渲染, +/- 行带浅背景, 不依赖次级 border。
import { useMemo, useState } from 'react'

interface EditDiffProps {
  content: string
  defaultCollapsed?: boolean
  filePath?: string
  /** 当 EditDiff 嵌套在已展开的 ActivityRow 中时隐藏自己的折叠头 */
  skipHeader?: boolean
}

interface DiffLine {
  kind: 'add' | 'del' | 'ctx' | 'fold' | 'sep' | 'header'
  text: string
  lineNo?: number
}

export function EditDiff({ content, defaultCollapsed = true, filePath, skipHeader }: EditDiffProps) {
  const [collapsed, setCollapsed] = useState(defaultCollapsed)
  const summary = useMemo(() => parseSummary(content, filePath), [content, filePath])
  const lines = useMemo(() => parseDiff(content), [content])

if (summary.bad) {
    return (
      <div className="border-t border-danger/20 px-3 pb-2 pt-2 font-mono text-[12px] leading-relaxed text-danger whitespace-pre-wrap">
        {content || '(无预览)'}
      </div>
    )
  }

  // skipHeader 模式下直接显示 diff 内容, 不包折叠按钮。
  // 不再单独 rounded-md bg-surface 做法; 融入 ActivityRow 容器, 用顶分隔线衔接。
  if (skipHeader) {
    return (
      <div className="overflow-x-auto border-t border-border/30 font-mono text-[12px] leading-relaxed">
        <table className="w-full border-collapse">
          <tbody>
            {lines.map((l, i) => (
              <tr key={i} className={rowClass(l)}>
                <td className="w-[3.5em] select-none px-1.5 pl-3 text-right text-text-3 text-[10px] tabular-nums">
                  {l.lineNo ?? ''}
                </td>
                <td className="w-[1.5em] select-none px-1 text-center text-[10px]">
                  {l.kind === 'add' ? '+' : l.kind === 'del' ? '−' : ' '}
                </td>
                <td className="whitespace-pre px-2 pr-3">{l.text}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    )
  }

  return (
    <div className="ml-5 mt-1">
      <button
        type="button"
        onClick={() => setCollapsed((v) => !v)}
        className="flex w-full items-center gap-2 rounded-md px-2 py-1 text-left text-[12px] hover:bg-surface-3/60"
      >
        <span className="font-mono text-text-3 text-[10px]">{collapsed ? '▸' : '▾'}</span>
        <span>✏️</span>
        <span className="font-mono text-text-2 truncate">{summary.path}</span>
        {summary.ok && (
          <span className="ml-auto flex items-center gap-1 font-mono text-[11px] tabular-nums">
            <span className="text-success">+{summary.add}</span>
            <span className="text-danger">−{summary.del}</span>
          </span>
        )}
      </button>

      {!collapsed && (
        <div className="mt-1 overflow-hidden rounded-md bg-surface font-mono text-[12px] leading-relaxed">
          <div className="overflow-x-auto">
            <table className="w-full border-collapse">
              <tbody>
                {lines.map((l, i) => (
                  <tr key={i} className={rowClass(l)}>
                    <td className="w-[3.5em] select-none px-1.5 text-right text-text-3 text-[10px] tabular-nums">
                      {l.lineNo ?? ''}
                    </td>
                    <td className="w-[1.5em] select-none px-1 text-center text-[10px]">
                      {l.kind === 'add' ? '+' : l.kind === 'del' ? '−' : ' '}
                    </td>
                    <td className="whitespace-pre px-2 pr-3">{l.text}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </div>
  )
}

function rowClass(l: DiffLine): string {
  switch (l.kind) {
    case 'add':    return 'bg-success/12'
    case 'del':    return 'bg-danger/12 text-text-2'
    case 'fold':   return 'text-text-3 italic'
    case 'header': return 'bg-surface-3/50 text-text-2'
    default:       return ''
  }
}

interface Summary {
  path: string
  add: number
  del: number
  ok: boolean
  bad?: boolean
}

function parseSummary(content: string, filePath?: string): Summary {
  const structured = parseStructuredDiff(content)
  if (structured) {
    let add = 0, del = 0
    for (const line of structured.lines) {
      if (line.kind === 'add') add++
      else if (line.kind === 'del') del++
    }
    return {
      path: filePath || structured.path || '(unknown)',
      add,
      del,
      ok: add + del > 0,
      bad: false,
    }
  }
  if (!content || (!content.startsWith('[') && !filePath)) {
    return { path: filePath ?? '(unknown)', add: 0, del: 0, ok: false, bad: true }
  }
  let path = filePath ?? ''
  const firstNl = content.indexOf('\n')
  const header = firstNl === -1 ? content : content.slice(0, firstNl)
  if (header.startsWith('[') && header.endsWith(']') && header.includes('#')) {
    const tag = header.slice(1, -1)
    const h = tag.lastIndexOf('#')
    path = h > 0 ? tag.slice(0, h) : tag
  } else if (!path) {
    path = header
  }
  let add = 0, del = 0
  for (const line of content.split('\n')) {
    const colon = line.indexOf(':')
    if (colon <= 0) continue
    const p = line.slice(0, colon).trimStart()
    if (p.startsWith('+')) add++
    else if (p.startsWith('-')) del++
  }
  return { path, add, del, ok: add + del > 0, bad: false }
}

function parseDiff(content: string): DiffLine[] {
  const structured = parseStructuredDiff(content)
  if (structured) {
    return structured.lines.map((l) => ({
      kind: l.kind,
      text: l.text,
      lineNo: l.line_no || undefined,
    }))
  }
  const out: DiffLine[] = []
  let sawSep = false
  for (const raw of content.split('\n')) {
    if (sawSep) break
    if (raw.startsWith('[') && raw.endsWith(']') && raw.includes('#')) {
      // skip the [path#hash] header line — it's internal, shown in ActivityRow
      continue
    }
    if (raw.trim() === '---') {
      // --- 是后端 preview 区段和正文之间的内部分隔标记, GUI 不显示。
      sawSep = true
      break
    }
    if (raw.startsWith('…')) {
      out.push({ kind: 'fold', text: raw })
      continue
    }
    const colon = raw.indexOf(':')
    if (colon <= 0) continue
    const prefix = raw.slice(0, colon).trimStart()
    const text = raw.slice(colon + 1)
    let lineNo = 0
    let kind: DiffLine['kind'] = 'ctx'
    if (prefix.startsWith('+')) {
      kind = 'add'
      lineNo = +prefix.slice(1) || 0
    } else if (prefix.startsWith('-')) {
      kind = 'del'
      lineNo = +prefix.slice(1) || 0
    } else {
      lineNo = +prefix || 0
    }
    out.push({ kind, text, lineNo })
  }
  return out
}

interface StructuredDiff {
  path: string
  lines: Array<{
    kind: DiffLine['kind']
    line_no?: number
    text: string
  }>
}

const STRUCTURED_MARKER = 'EDIT_PREVIEW_JSON_B64 '

function parseStructuredDiff(content: string): StructuredDiff | null {
  const line = content.split('\n').find((l) => l.startsWith(STRUCTURED_MARKER))
  if (!line) return null
  try {
    const encoded = line.slice(STRUCTURED_MARKER.length).trim()
    const bytes = Uint8Array.from(atob(encoded), (c) => c.charCodeAt(0))
    const parsed = JSON.parse(new TextDecoder().decode(bytes)) as StructuredDiff
    if (!parsed || !Array.isArray(parsed.lines)) return null
    return parsed
  } catch {
    return null
  }
}
