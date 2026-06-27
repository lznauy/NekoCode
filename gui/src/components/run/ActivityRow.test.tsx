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

  it('renders edit revert output as plain tool information', () => {
    const step: ToolStep = {
      id: 'edit-revert',
      toolName: 'edit',
      args: 'path=/tmp/file.go,revert=true',
      output: '[/tmp/file.go#TAG]\nReverted to pre-edit state (latest snapshot).\nNote: edit keeps one latest pre-edit snapshot per file.',
      status: 'done',
      isError: false,
      collapsed: false,
    }
    const toggleStep = vi.fn()

    render(<ActivityRow step={step} toggleStep={toggleStep} />)

    expect(screen.getByText(/Reverted to pre-edit state/)).toBeInTheDocument()
    expect(screen.getByText(/keeps one latest pre-edit snapshot/)).toBeInTheDocument()
    expect(screen.queryByRole('table')).toBeNull()
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
  })
})
