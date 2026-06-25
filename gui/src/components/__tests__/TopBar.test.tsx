import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { TopBar } from '../TopBar'

// 主题切换 props 默认暗色, 回调为空函数。
const themeProps = { theme: 'dark' as const, onToggleTheme: vi.fn(), onClose: vi.fn() }

describe('TopBar', () => {
  it('renders logo NekoCode (split across spans, text combined)', () => {
    render(<TopBar model="" busy={false} {...themeProps} />)
    expect(screen.getByText('Neko')).toBeInTheDocument()
    expect(screen.getByText('Code')).toBeInTheDocument()
  })

  it('shows model badge when model is provided', () => {
    render(<TopBar model="openai / gpt-4" busy={false} {...themeProps} />)
    expect(screen.getByText('openai / gpt-4')).toBeInTheDocument()
  })

  it('does not show badge when model is empty', () => {
    render(<TopBar model="" busy={false} {...themeProps} />)
    expect(screen.queryByText(/openai/)).toBeNull()
  })

  it('shows 就绪 status when not busy', () => {
    render(<TopBar model="" busy={false} {...themeProps} />)
    expect(screen.getByText('就绪')).toBeInTheDocument()
  })

  it('shows 思考中 status when busy', () => {
    render(<TopBar model="" busy={true} {...themeProps} />)
    expect(screen.getByText('思考中')).toBeInTheDocument()
  })

  it('renders status dot with correct color/class', () => {
    const { container, rerender } = render(<TopBar model="" busy={false} {...themeProps} />)
    // 状态点是 h-1.5 w-1.5 rounded-full; 主题切换按钮是 rounded-md, 不在 .rounded-full 选择范围。
    const dot = container.querySelector('.rounded-full')
    expect(dot).toHaveClass('bg-success')

    rerender(<TopBar model="" busy={true} {...themeProps} />)
    const dotBusy = container.querySelector('.rounded-full')
    expect(dotBusy).toHaveClass('bg-primary')
    expect(dotBusy).toHaveClass('animate-pulse-soft')
  })

  it('calls onClose when close button is clicked', () => {
    const onClose = vi.fn()
    render(<TopBar model="" busy={false} theme="dark" onToggleTheme={vi.fn()} onClose={onClose} />)

    fireEvent.click(screen.getByRole('button', { name: '关闭窗口' }))

    expect(onClose).toHaveBeenCalledTimes(1)
  })
})
