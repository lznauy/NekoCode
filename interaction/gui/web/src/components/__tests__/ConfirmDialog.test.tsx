import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import ConfirmDialog from '../ConfirmDialog'

const replyConfirm = vi.fn()

vi.mock('../../lib/wails', () => ({
  safeReplyConfirm: (id: string, ok: boolean, remember?: boolean) => replyConfirm(id, ok, remember),
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
          toolName: 'shell',
          args: { command },
          kind: 'permission',
        }}
        onDone={onDone}
      />,
    )

    expect(screen.getByText('确认执行命令')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '拒绝' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '仅本次允许' })).toBeInTheDocument()
    expect(screen.queryByText('调用参数')).toBeNull()
    expect(screen.getAllByText((text) => text.includes('cat > /tmp/test_edit.txt'))).toHaveLength(1)

    fireEvent.click(screen.getByRole('button', { name: '仅本次允许' }))
    expect(replyConfirm).toHaveBeenCalledWith('confirm-1', true, false)
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
          kind: 'permission',
        }}
        onDone={vi.fn()}
      />,
    )

    expect(screen.getByText(/replaceAll 将替换 25 处精确匹配/)).toBeInTheDocument()
    expect(screen.getByText(/请重点确认范围/)).toBeInTheDocument()
  })

  it('shows revert preview as diff content', () => {
    render(
      <ConfirmDialog
        entry={{
          id: 'confirm-revert',
          toolName: 'edit',
          args: { path: '/tmp/file.go', revert: true },
          preview: '[/tmp/file.go#revert]\n-1:changed\n+1:original\n',
          kind: 'permission',
        }}
        onDone={vi.fn()}
      />,
    )

    expect(screen.getByRole('table')).toBeInTheDocument()
    expect(screen.getByText('changed')).toBeInTheDocument()
    expect(screen.getByText('original')).toBeInTheDocument()
    expect(screen.getByText('上方差异是本次 revert 将恢复的内容。')).toBeInTheDocument()
  })

  it('shows permission details and can remember project grants', () => {
    render(
      <ConfirmDialog
        entry={{
          id: 'confirm-permission',
          toolName: 'shell',
          args: {
            command: 'go test ./...',
            commandClass: 'network',
          },
          kind: 'permission',
          approval: {
            reason: 'command requires public network access',
            capabilities: ['net.public', 'cache.write'],
            scope: 'project',
            workspace: '/repo',
            sandbox: 'bubblewrap',
            write_paths: ['/tmp/cache'],
            outbound_domains: ['registry.npmjs.org'],
          },
        }}
        onDone={vi.fn()}
      />,
    )

    expect(screen.getByText('审批详情')).toBeInTheDocument()
    expect(screen.getByText('command requires public network access')).toBeInTheDocument()
    expect(screen.getByText('/tmp/cache')).toBeInTheDocument()
    expect(screen.getByText('registry.npmjs.org')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '仅本次允许' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '始终允许' }))
    expect(replyConfirm).toHaveBeenCalledWith('confirm-permission', true, true)
  })

  it('combines command and predictable capability approval', () => {
    render(
      <ConfirmDialog
        entry={{
          id: 'confirm-bash',
          toolName: 'shell',
          args: {
            command: 'go get github.com/hajimehoshi/ebiten/v2',
          },
          kind: 'permission',
          approval: {
            reason: 'command requires public network access',
            capabilities: ['net.public'],
            scope: 'project',
            combined: true,
            structures: ['command_substitution'],
          },
        }}
        onDone={vi.fn()}
      />,
    )

    expect(screen.getByText('命令替换')).toBeInTheDocument()
    expect(screen.queryByText('始终允许并授权')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: '始终允许' }))
    expect(replyConfirm).toHaveBeenCalledWith('confirm-bash', true, true)
  })

  it('hides remember for once-only capability approval', () => {
    render(
      <ConfirmDialog
        entry={{
          id: 'confirm-bash-plain',
          toolName: 'shell',
          args: {
            command: 'go get github.com/hajimehoshi/ebiten/v2',
          },
          kind: 'permission',
          approval: {
            reason: 'host execution required',
            scope: 'once',
          },
        }}
        onDone={vi.fn()}
      />,
    )

    expect(screen.queryByRole('button', { name: '始终允许' })).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: '仅本次允许' }))
    expect(replyConfirm).toHaveBeenCalledWith('confirm-bash-plain', true, false)
  })

  it('describes dynamic command approval without inventing capabilities', () => {
    render(
      <ConfirmDialog
        entry={{
          id: 'confirm-dynamic',
          toolName: 'shell',
          args: { command: 'env -S "bash -c echo"' },
          kind: 'permission',
          approval: {
            risk: 'dynamic shell execution',
            reason: 'dynamic shell execution',
            scope: 'project',
            structures: ['dynamic_command'],
          },
        }}
        onDone={vi.fn()}
      />,
    )

    expect(screen.getByText('此决定可记住为该命令的精确授权。')).toBeInTheDocument()
    expect(screen.queryByText(/列出的.*能力/)).toBeNull()
  })
})
