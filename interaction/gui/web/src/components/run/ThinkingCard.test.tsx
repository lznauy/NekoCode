import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { ThinkingCard } from './ThinkingCard'

describe('ThinkingCard', () => {
  it('mounts the latest reasoning only when expanded and renders markdown when done', () => {
    const { container, rerender } = render(<ThinkingCard reasoning="first draft" done={false} />)
    const details = container.querySelector('details')!
    expect(screen.queryByText('first draft')).not.toBeInTheDocument()

    details.open = true
    fireEvent(details, new Event('toggle'))
    expect(screen.getByText('first draft')).toBeInTheDocument()
    expect(container.querySelector('.markdown')).toBeNull()

    details.open = false
    fireEvent(details, new Event('toggle'))
    rerender(<ThinkingCard reasoning="**final reasoning**" done />)
    expect(screen.queryByText('final reasoning')).not.toBeInTheDocument()
    expect(container.querySelector('.markdown')).toBeNull()

    details.open = true
    fireEvent(details, new Event('toggle'))
    expect(screen.getByText('final reasoning').tagName).toBe('STRONG')
  })
})
