import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { CompactionDetails } from './CompactionDetails'
import type { CompactionEvent } from '../../types/events'

const event: CompactionEvent = { id: 'compact-1', status: 'started', trigger: 'auto', beforeTokens: 10000, afterTokens: 0, beforeMessages: 20, afterMessages: 0, elapsedMs: 0 }

describe('compaction details', () => {
  it('streams expanded content, respects toggles, and preserves the final summary', () => {
    const { rerender } = render(<CompactionDetails event={event} />)
    expect(screen.getByRole('button')).toHaveAttribute('aria-expanded', 'true')
    rerender(<CompactionDetails event={{ ...event, status: 'delta', summary: '<summary>已完成修改' }} />)
    expect(screen.getByText('已完成修改')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button'))
    rerender(<CompactionDetails event={{ ...event, status: 'delta', summary: '<summary>已完成修改，继续测试' }} />)
    expect(screen.getByRole('button')).toHaveAttribute('aria-expanded', 'false')
    rerender(<CompactionDetails event={{ ...event, status: 'completed', summary: '最终摘要', afterTokens: 2000, afterMessages: 6, elapsedMs: 1200 }} />)
    expect(screen.queryByText('最终摘要')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button'))
    expect(screen.getByText('最终摘要')).toBeInTheDocument()
    expect(screen.getByText(/20 → 6 条消息/)).toBeInTheDocument()
  })

  it('shows failure and partial summary without rendering HTML', () => {
    render(<CompactionDetails event={{ ...event, status: 'failed', error: 'stream interrupted', summary: '<script>alert(1)</script>' }} />)
    expect(screen.getByText('stream interrupted')).toBeInTheDocument()
    expect(screen.getByText('<script>alert(1)</script>')).toBeInTheDocument()
    expect(document.querySelector('script')).toBeNull()
  })

  it('shows a warning when completed compaction still exceeds budget', () => {
    render(<CompactionDetails event={{ ...event, status: 'completed', warning: 'context full: 90000 tokens used of 80000 budget', summary: '最终摘要', afterTokens: 2000, afterMessages: 6, elapsedMs: 1200 }} />)
    fireEvent.click(screen.getByRole('button'))
    expect(screen.getByText(/context full/)).toBeInTheDocument()
    expect(screen.getByText('最终摘要')).toBeInTheDocument()
  })
})
