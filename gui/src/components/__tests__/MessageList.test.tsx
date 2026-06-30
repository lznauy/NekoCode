import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { MessageList } from '../MessageList'
import type { Msg } from '../../types/events'

describe('MessageList', () => {
  it('shows the new conversation empty state when there are no messages', () => {
    render(<MessageList msgs={[]} endRef={{ current: null }} onPromptSelect={vi.fn()} toggleStep={vi.fn()} />)

    expect(screen.getByText('从一个具体任务开始')).toBeInTheDocument()
    expect(screen.getByText('了解项目')).toBeInTheDocument()
  })

  it('uses prompt starters as real actions', () => {
    const onPromptSelect = vi.fn()
    render(<MessageList msgs={[]} endRef={{ current: null }} onPromptSelect={onPromptSelect} toggleStep={vi.fn()} />)

    fireEvent.click(screen.getByText('排查失败'))

    expect(onPromptSelect).toHaveBeenCalledWith('排查最近一次失败的测试或构建，并给出最小修复方案')
  })

  it('renders active and short conversations in normal flow', () => {
    const msgs: Msg[] = [
      { id: 'u1', role: 'user', text: 'hello', streaming: false },
      { id: 'a1', role: 'assistant', text: 'hi', streaming: false },
    ]
    const endRef = { current: null as HTMLDivElement | null }
    const { container } = render(<MessageList msgs={msgs} endRef={endRef} onPromptSelect={vi.fn()} toggleStep={vi.fn()} />)
    expect(screen.getByText('hello')).toBeInTheDocument()
    expect(screen.getByText('hi')).toBeInTheDocument()
    expect(container.querySelector('[data-testid="virtual-message-list"]')).toBeNull()
    expect(endRef.current).toBeInstanceOf(HTMLDivElement)
  })

  it('virtualizes long static histories', () => {
    const msgs: Msg[] = Array.from({ length: 90 }, (_, i) => ({
      id: `m${i}`,
      role: i % 2 === 0 ? 'user' : 'assistant',
      text: `message ${i}`,
      streaming: false,
    }))
    const { container } = render(<MessageList msgs={msgs} endRef={{ current: null }} onPromptSelect={vi.fn()} toggleStep={vi.fn()} />)

    expect(container.querySelector('[data-testid="virtual-message-list"]')).toBeTruthy()
  })

  it('does not virtualize long histories while a run is streaming', () => {
    const msgs: Msg[] = Array.from({ length: 90 }, (_, i) => ({
      id: `m${i}`,
      role: i % 2 === 0 ? 'user' : 'assistant',
      text: `message ${i}`,
      streaming: i === 89,
    }))
    const { container } = render(<MessageList msgs={msgs} endRef={{ current: null }} onPromptSelect={vi.fn()} toggleStep={vi.fn()} />)

    expect(container.querySelector('[data-testid="virtual-message-list"]')).toBeNull()
  })
})
