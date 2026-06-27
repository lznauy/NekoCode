// toolicons: 工具名 → emoji 图标。沿用 TUI ProcessingItem 语义但更克制。
const ICONS: Record<string, string> = {
  read: '📖',
  edit: '✎',
  write: '✍',
  bash: '⌘',
  list: '☰',
  grep: '⊸',
  glob: '🔍',
  todo: '☰',
  webfetch: '🕸',
  think: '💭',
  tsread: '📖',
  tslist: '☰',
  searchfiles: '🔍',
  fetch: '🕸',
}

const FANCY_NAMES: Record<string, string> = {
  read: 'read',
  edit: 'edit',
  write: 'write',
  bash: 'bash',
  list: 'list',
  grep: 'grep',
  glob: 'glob',
  todo: 'todo',
  webfetch: 'fetch',
  fetch: 'fetch',
}

export function toolIcon(name: string): string {
  return ICONS[name] ?? '•'
}

export function prettyTool(name: string): string {
  return FANCY_NAMES[name] ?? (name || 'tool')
}

// 简洁参数: 单行截断。
// bash: 直接展开 command 字段 (命令本身即核心信息), 放宽至 120 字符;
// 其余工具: key: value 形式, 过滤 _editCache 等内部字段, 56 字符截断。
export function compactArgs(args: string): string {
  const s = args.trim()
  if (!s) return ''
  const parsed = parseArgsPayload(s)
  if (parsed) {
    // bash 命令: command 字段即核心, 去掉冗余前缀, 给更多空间
    if (typeof parsed.command === 'string') {
      const cmd = parsed.command.trim()
      if (!cmd) return ''
      if (cmd.length > 120) return cmd.slice(0, 117) + '…'
      return cmd
    }
    if (typeof parsed.path === 'string' && parsed.oldString !== undefined) {
      return parsed.path
    }
    const keys = Object.keys(parsed).filter((k) => !k.startsWith('_'))
    if (keys.length === 0) return ''
    const display = keys.map((k) => `${k}: ${JSON.stringify(parsed[k])}`).join(', ')
    if (display.length > 56) return display.slice(0, 53) + '…'
    return display
  }
  // 原始字符串: 可能本身就是命令 (历史会话/非 JSON 载荷)
  if (s.length > 120) return s.slice(0, 117) + '…'
  return s
}

export function pathFromArgs(args: string): string {
  const parsed = parseArgsPayload(args.trim())
  return typeof parsed?.path === 'string' ? parsed.path : args
}

function parseArgsPayload(s: string): Record<string, unknown> | null {
  if (!s) return null
  try {
    const parsed = JSON.parse(s)
    if (typeof parsed === 'object' && parsed !== null && !Array.isArray(parsed)) {
      return parsed as Record<string, unknown>
    }
  } catch {
    // fall through to key=value parser
  }
  const out: Record<string, string> = {}
  for (const pair of splitPairs(s)) {
    const idx = pair.indexOf('=')
    if (idx <= 0) continue
    const key = pair.slice(0, idx).trim()
    if (!key) continue
    out[key] = unquoteArgValue(pair.slice(idx + 1).trim())
  }
  return Object.keys(out).length ? out : null
}

function splitPairs(s: string): string[] {
  const pairs: string[] = []
  let start = 0
  let inQuote = false
  for (let i = 0; i < s.length; i++) {
    const ch = s[i]
    if (ch === '"' && !isEscaped(s, i)) {
      inQuote = !inQuote
    } else if (ch === ',' && !inQuote) {
      pairs.push(s.slice(start, i))
      start = i + 1
    }
  }
  pairs.push(s.slice(start))
  return pairs
}

function unquoteArgValue(value: string): string {
  if (value.length >= 2 && value[0] === '"' && value[value.length - 1] === '"') {
    return value.slice(1, -1).replace(/\\"/g, '"').replace(/\\\\/g, '\\')
  }
  return value
}

function isEscaped(s: string, idx: number): boolean {
  let count = 0
  for (let i = idx - 1; i >= 0 && s[i] === '\\'; i--) count++
  return count % 2 === 1
}

// edit summary: 从 diff preview 中抽出 "+N -M" 摘要或路径。
export function editSummary(content?: string): string {
  if (!content) return ''
  const lines = content.split('\n')
  let add = 0, del = 0
  for (const l of lines) {
    const colon = l.indexOf(':')
    if (colon <= 0) continue
    const prefix = l.slice(0, colon).trimStart()
    if (prefix.startsWith('+')) add++
    else if (prefix.startsWith('-')) del++
  }
  if (add || del) return `+${add} −${del}`
  return ''
}
