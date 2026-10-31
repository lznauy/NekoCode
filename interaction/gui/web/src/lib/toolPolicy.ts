export function persistentTool(name: string): boolean {
  return name === 'edit' || name === 'diff' || name === 'shell' || name === 'bash' || name === 'process' || name === 'write'
}
