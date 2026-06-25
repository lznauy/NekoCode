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
  try {
    const parsed = JSON.parse(s)
    if (typeof parsed === 'object' && parsed !== null && !Array.isArray(parsed)) {
      // bash 命令: command 字段即核心, 去掉冗余前缀, 给更多空间
      if (typeof parsed.command === 'string') {
        const cmd = parsed.command.trim()
        if (!cmd) return ''
        if (cmd.length > 120) return cmd.slice(0, 117) + '…'
        return cmd
      }
      const keys = Object.keys(parsed).filter((k) => !k.startsWith('_'))
      if (keys.length === 0) return ''
      const display = keys.map((k) => `${k}: ${JSON.stringify(parsed[k])}`).join(', ')
      if (display.length > 56) return display.slice(0, 53) + '…'
      return display
    }
  } catch {
    // not JSON, show raw
  }
  // 原始字符串: 可能本身就是命令 (历史会话/非 JSON 载荷)
  if (s.length > 120) return s.slice(0, 117) + '…'
  return s
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