import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { ActivityRow } from './ActivityRow'
import type { ToolStep } from '../../types/events'

describe('ActivityRow', () => {
  it('renders edit failure output as an error message', () => {
    const step: ToolStep = {
      id: 'edit-1',
      toolName: 'edit',
      args: 'path=/tmp/file.go,oldString=missing,newString=next',
      output: 'oldString was not found',
      status: 'error',
      isError: true,
      collapsed: false,
    }

    const toggleStep = vi.fn()

    render(<ActivityRow step={step} toggleStep={toggleStep} />)

    expect(screen.getByText('oldString was not found')).toBeInTheDocument()
    expect(screen.queryByRole('table')).toBeNull()
  })

  it('renders edit revert output as a diff table', () => {
    const step: ToolStep = {
      id: 'edit-revert',
      toolName: 'edit',
      args: 'path=/tmp/file.go,revert=true',
      output: '[/tmp/file.go#TAG]\n-1:changed\n+1:original\n',
      status: 'done',
      isError: false,
      collapsed: false,
    }
    const toggleStep = vi.fn()

    render(<ActivityRow step={step} toggleStep={toggleStep} />)

    expect(screen.getByRole('table')).toBeInTheDocument()
    expect(screen.getByText('changed')).toBeInTheDocument()
    expect(screen.getByText('original')).toBeInTheDocument()
  })

  it('calls toggleStep when the expand/collapse button is clicked', async () => {
    const step: ToolStep = {
      id: 'bash-1',
      toolName: 'bash',
      args: 'command=echo hello',
      output: 'hello\nworld',
      status: 'done',
      isError: false,
      collapsed: false,
    }
    const toggleStep = vi.fn()

    render(<ActivityRow step={step} toggleStep={toggleStep} />)
    const button = screen.getByRole('button')
    await userEvent.click(button)

    expect(toggleStep).toHaveBeenCalledTimes(1)
    expect(toggleStep).toHaveBeenCalledWith('bash-1')
    expect(button).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByText('完成')).toBeInTheDocument()
  })

  it('renders blocked tools as blocked instead of execution errors', () => {
    const step: ToolStep = {
      id: 'bash-blocked',
      toolName: 'bash',
      args: 'command=rm -rf /tmp/example',
      output: 'command was blocked by policy',
      status: 'blocked',
      isError: true,
      collapsed: false,
    }
    const toggleStep = vi.fn()

    render(<ActivityRow step={step} toggleStep={toggleStep} />)

    expect(screen.getByText('阻止')).toBeInTheDocument()
    expect(screen.queryByText('错误')).toBeNull()
    expect(screen.getByText('command was blocked by policy')).toBeInTheDocument()
  })

  it('renders MCP calls with server and tool names plus output', () => {
    const step: ToolStep = {
      id: 'mcp-1',
      toolName: 'nixos__search',
      args: '{"query":"flakes"}',
      output: 'flake documentation',
      status: 'done',
      isError: false,
      collapsed: false,
    }
    const toggleStep = vi.fn()

    render(<ActivityRow step={step} toggleStep={toggleStep} />)

    expect(screen.getByText('MCP · nixos')).toBeInTheDocument()
    expect(screen.getByText('search')).toBeInTheDocument()
    expect(screen.getByText('query: "flakes"')).toBeInTheDocument()
    expect(screen.getByText('flake documentation')).toBeInTheDocument()
  })

  it('renders write completion with tag but no diff as plain text', () => {
    const step: ToolStep = {
      id: 'write-no-diff',
      toolName: 'write',
      args: 'path=/tmp/file.go,content=same',
      output: '[/tmp/file.go#TAG] Written (4 chars)',
      status: 'done',
      isError: false,
      collapsed: false,
    }
    const toggleStep = vi.fn()

    render(<ActivityRow step={step} toggleStep={toggleStep} />)

    expect(screen.getByText('[/tmp/file.go#TAG] Written (4 chars)')).toBeInTheDocument()
    expect(screen.queryByRole('table')).toBeNull()
  })

  it('renders write diff output as a diff table', () => {
    const step: ToolStep = {
      id: 'write-diff',
      toolName: 'write',
      args: 'path=/tmp/file.go,content=next',
      output: '[/tmp/file.go#TAG]\n[write /tmp/file.go]\n-1:old\n+1:next\n',
      status: 'done',
      isError: false,
      collapsed: false,
    }
    const toggleStep = vi.fn()

    render(<ActivityRow step={step} toggleStep={toggleStep} />)

    expect(screen.getByRole('table')).toBeInTheDocument()
    expect(screen.getByText('old')).toBeInTheDocument()
    expect(screen.getByText('next')).toBeInTheDocument()
    expect(screen.queryByText('[write /tmp/file.go]')).toBeNull()
  })
})
