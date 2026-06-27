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

  it('renders messages via virtualizer without crashing', () => {
    const msgs: Msg[] = [
      { id: 'u1', role: 'user', text: 'hello', streaming: false },
      { id: 'a1', role: 'assistant', text: 'hi', streaming: false },
    ]
    const endRef = { current: null as HTMLDivElement | null }
    const { container } = render(<MessageList msgs={msgs} endRef={endRef} onPromptSelect={vi.fn()} toggleStep={vi.fn()} />)
    // jsdom 无真实 layout, 虚拟器不渲染任何条目 (可见范围为空),
    // 但虚拟容器 (height=estimateSize * count) 与 endRef 锚点都已正确挂载。
    const virtualContainer = container.querySelector('div[style*="position: relative"]')
    expect(virtualContainer).toBeTruthy()
    expect(virtualContainer?.getAttribute('style')).toContain('height')
    expect(endRef.current).toBeInstanceOf(HTMLDivElement)
  })
})
