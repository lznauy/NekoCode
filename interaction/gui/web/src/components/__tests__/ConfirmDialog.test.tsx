import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ConfirmDialog from '../ConfirmDialog'

const replyConfirm = vi.fn()

vi.mock('../../lib/wails', () => ({
  safeReplyConfirm: (id: string, ok: boolean, remember?: boolean) => replyConfirm(id, ok, remember),
}))

describe('ConfirmDialog', () => {
  beforeEach(() => replyConfirm.mockReset())
  it('waits for acknowledgement and prevents duplicate submission', async () => {
    let resolve!: () => void
    replyConfirm.mockReturnValueOnce(new Promise<void>((done) => { resolve = done }))
    const onDone = vi.fn()
    render(<ConfirmDialog entry={{ id: 'pending', toolName: 'shell', args: { command: 'make all' }, kind: 'permission' }} onDone={onDone} />)
    fireEvent.click(screen.getByRole('button', { name: '仅本次允许' }))
    fireEvent.keyDown(window, { key: 'Enter' })
    expect(replyConfirm).toHaveBeenCalledTimes(1)
    expect(onDone).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: '拒绝' })).toBeDisabled()
    await act(async () => resolve())
    expect(onDone).toHaveBeenCalledTimes(1)
  })

  it('keeps a failed submission open and allows retry', async () => {
    replyConfirm.mockRejectedValueOnce(new Error('transport failed'))
    const onDone = vi.fn()
    render(<ConfirmDialog entry={{ id: 'retry', toolName: 'shell', args: {}, kind: 'permission' }} onDone={onDone} />)
    fireEvent.click(screen.getByRole('button', { name: '仅本次允许' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('提交未成功')
    expect(onDone).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: '拒绝' }))
    await waitFor(() => expect(onDone).toHaveBeenCalledTimes(1))
    expect(replyConfirm).toHaveBeenLastCalledWith('retry', false, false)
  })

  it('binds keyboard decisions to the current request and resets selection', async () => {
    const onDone = vi.fn()
    const { rerender } = render(<ConfirmDialog entry={{ id: 'old', toolName: 'shell', args: {}, kind: 'permission' }} onDone={onDone} />)
    fireEvent.keyDown(window, { key: 'ArrowDown' })
    rerender(<ConfirmDialog entry={{ id: 'new', toolName: 'shell', args: {}, kind: 'permission' }} onDone={onDone} />)
    fireEvent.keyDown(window, { key: 'Enter' })
    await waitFor(() => expect(onDone).toHaveBeenCalledTimes(1))
    expect(replyConfirm).toHaveBeenCalledWith('new', true, false)
  })

  it('does not close a new request when an old submission finishes', async () => {
    let resolve!: () => void
    replyConfirm.mockReturnValueOnce(new Promise<void>((done) => { resolve = done }))
    const onDone = vi.fn()
    const { rerender } = render(<ConfirmDialog entry={{ id: 'old', toolName: 'shell', args: {} }} onDone={onDone} />)
    fireEvent.click(screen.getByRole('button', { name: '仅本次允许' }))
    rerender(<ConfirmDialog entry={{ id: 'new', toolName: 'shell', args: {} }} onDone={onDone} />)
    await act(async () => resolve())
    expect(onDone).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: '仅本次允许' })).toBeEnabled()
  })

  it('previews the actual remembered literal and project', () => {
    render(<ConfirmDialog entry={{ id: 'literal', toolName: 'shell', args: {}, kind: 'permission', approval: { source: 'user_rule', rule: 'shell(*)', scope: 'project', remember_workspace: '/repo', remember_rules: [{ tool: 'shell', literal: "bash -c 'echo ok'" }] } }} onDone={vi.fn()} />)
    expect(screen.getByText('完整命令，仅精确匹配')).toBeInTheDocument()
    expect(screen.getByText("bash -c 'echo ok'")).toBeInTheDocument()
    expect(screen.getByText('生效项目：/repo')).toBeInTheDocument()
    expect(screen.getByText('shell(*)')).toBeInTheDocument()
    expect(screen.queryByText('未提供 JEV 判定')).not.toBeInTheDocument()
  })
  it('states that JEV judgment failed instead of implying a dangerous verdict', () => {
    render(<ConfirmDialog entry={{ id: 'jev-failed', toolName: 'shell', args: { command: 'make all' }, kind: 'permission', approval: { risk: 'jev: judgment failed', reason: 'jev: judgment failed', scope: 'project' } }} onDone={vi.fn()} />)
    expect(screen.getByText('jev判断失败，请手动决定。')).toBeInTheDocument()
  })
  it('shows bash command once and keeps decision buttons visible', async () => {
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
    await waitFor(() => expect(onDone).toHaveBeenCalledTimes(1))
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

  it('shows permission details and can remember project grants', async () => {
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
    expect(screen.getByText(/命令需要访问公共网络/)).toBeInTheDocument()
    expect(screen.getByText('/tmp/cache')).toBeInTheDocument()
    expect(screen.getByText('registry.npmjs.org')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '仅本次允许' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '始终允许' }))
    expect(replyConfirm).toHaveBeenCalledWith('confirm-permission', true, true)
    await waitFor(() => expect(screen.queryByRole('status')).toBeNull())
  })

  it('combines command and predictable capability approval', async () => {
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
    await waitFor(() => expect(screen.queryByRole('status')).toBeNull())
  })

  it('hides remember for once-only capability approval', async () => {
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
    await waitFor(() => expect(screen.queryByRole('status')).toBeNull())
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

    expect(screen.getByText('始终允许的范围')).toBeInTheDocument()
    expect(screen.queryByText(/列出的.*能力/)).toBeNull()
  })
})
