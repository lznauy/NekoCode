import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import ConfirmDialog from '../ConfirmDialog'

const replyConfirm = vi.fn()

vi.mock('../../lib/wails', () => ({
  safeReplyConfirm: (id: string, ok: boolean) => replyConfirm(id, ok),
}))

describe('ConfirmDialog', () => {
  it('shows bash command once and keeps decision buttons visible', () => {
    const command = [
      "cat > /tmp/test_edit.txt << 'EOF'",
      'line 1: hello world',
      'line 2: foo bar',
      'EOF',
      'echo "created"',
    ].join('\n')
    const onDone = vi.fn()

    render(
      <ConfirmDialog
        entry={{
          id: 'confirm-1',
          toolName: 'bash',
          args: { command },
          level: 1,
        }}
        onDone={onDone}
      />,
    )

    expect(screen.getByText('确认执行命令')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '拒绝' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '允许执行' })).toBeInTheDocument()
    expect(screen.queryByText('调用参数')).toBeNull()
    expect(screen.getAllByText((text) => text.includes('cat > /tmp/test_edit.txt'))).toHaveLength(1)

    fireEvent.click(screen.getByRole('button', { name: '允许执行' }))
    expect(replyConfirm).toHaveBeenCalledWith('confirm-1', true)
    expect(onDone).toHaveBeenCalledTimes(1)
  })

  it('shows replaceAll replacement count and warning', () => {
    render(
      <ConfirmDialog
        entry={{
          id: 'confirm-edit',
          toolName: 'edit',
          args: { path: '/tmp/file.go', oldString: 'foo', newString: 'bar', replaceAll: true },
          preview: '(25 replacements)\n-1:foo\n+1:bar',
          level: 1,
        }}
        onDone={vi.fn()}
      />,
    )

    expect(screen.getByText(/replaceAll 将替换 25 处精确匹配/)).toBeInTheDocument()
    expect(screen.getByText(/请重点确认范围/)).toBeInTheDocument()
  })

  it('shows revert preview as plain tool content', () => {
    render(
      <ConfirmDialog
        entry={{
          id: 'confirm-revert',
          toolName: 'edit',
          args: { path: '/tmp/file.go', revert: true },
          preview: '(revert: file.go)',
          level: 1,
        }}
        onDone={vi.fn()}
      />,
    )

    expect(screen.getByText('(revert: file.go)')).toBeInTheDocument()
    expect(screen.getByText('允许后将恢复该文件最近一次 edit 前的快照。')).toBeInTheDocument()
    expect(screen.queryByRole('table')).toBeNull()
  })
})
