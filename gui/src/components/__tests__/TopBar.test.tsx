import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { TopBar } from '../TopBar'

// 主题切换 props 默认暗色, 回调为空函数。
const themeProps = { theme: 'dark' as const, onToggleTheme: vi.fn(), onOpenConfig: vi.fn(), onOpenSkills: vi.fn(), onClose: vi.fn() }

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
    const { container } = render(<TopBar model="" busy={true} {...themeProps} />)
    expect(screen.getByText('思考中')).toBeInTheDocument()
    expect(container.querySelector('svg.animate-spin')).toBeTruthy()
  })

  it('renders status badge with correct state', () => {
    const { rerender } = render(<TopBar model="" busy={false} {...themeProps} />)
    expect(screen.getByText('就绪').closest('span')).toHaveClass('text-success')

    rerender(<TopBar model="" busy={true} {...themeProps} />)
    expect(screen.getByText('思考中').closest('span')).toHaveClass('text-primary')
  })

  it('calls onClose when close button is clicked', () => {
    const onClose = vi.fn()
    render(<TopBar model="" busy={false} theme="dark" onToggleTheme={vi.fn()} onOpenConfig={vi.fn()} onOpenSkills={vi.fn()} onClose={onClose} />)

    fireEvent.click(screen.getByRole('button', { name: '关闭窗口' }))

    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('calls onOpenConfig when config button is clicked', () => {
    const onOpenConfig = vi.fn()
    render(<TopBar model="" busy={false} theme="dark" onToggleTheme={vi.fn()} onOpenConfig={onOpenConfig} onOpenSkills={vi.fn()} onClose={vi.fn()} />)

    fireEvent.click(screen.getByRole('button', { name: '配置管理' }))

    expect(onOpenConfig).toHaveBeenCalledTimes(1)
  })

  it('calls onOpenSkills when skill button is clicked', () => {
    const onOpenSkills = vi.fn()
    render(<TopBar model="" busy={false} theme="dark" onToggleTheme={vi.fn()} onOpenConfig={vi.fn()} onOpenSkills={onOpenSkills} onClose={vi.fn()} />)

    fireEvent.click(screen.getByRole('button', { name: '技能管理' }))

    expect(onOpenSkills).toHaveBeenCalledTimes(1)
  })
})
