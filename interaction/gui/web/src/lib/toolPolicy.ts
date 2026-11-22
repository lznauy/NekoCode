export function persistentTool(name: string): boolean {
  return name === 'task_batch' || name === 'edit' || name === 'diff' || name === 'shell' || name === 'bash' || name === 'process' || name === 'write'
}
