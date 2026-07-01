export function isUnifiedDiffContent(content: string): boolean {
  if (!content) return false
  const firstLine = content.split('\n', 1)[0] ?? ''
  if (/^\[[^\]]+#[^\]]+\]$/.test(firstLine)) return true
  if (/^\[write [^\]]+\]$/.test(firstLine)) return true
  return content.split('\n').some((line) => /^[+-]\d+:/.test(line))
}
