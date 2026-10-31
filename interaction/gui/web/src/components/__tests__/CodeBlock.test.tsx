import { fireEvent, render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { CodeBlock } from '../CodeBlock'
it.each([undefined, { writeText: vi.fn().mockRejectedValue(new Error('denied')) }])('does not claim success without clipboard acknowledgment', async (clipboard) => {
 Object.defineProperty(navigator, 'clipboard', { value: clipboard, configurable: true })
 render(<CodeBlock code="hello" lang="text" />)
 fireEvent.click(screen.getByRole('button', { name: '复制' }))
 expect(await screen.findByRole('alert')).toBeInTheDocument()
 expect(screen.queryByText('已复制')).not.toBeInTheDocument()
})
it('shows success after clipboard acknowledgment', async () => {
 Object.defineProperty(navigator, 'clipboard', { value: { writeText: vi.fn().mockResolvedValue(undefined) }, configurable: true })
 render(<CodeBlock code="hello" lang="text" />)
 fireEvent.click(screen.getByRole('button', { name: '复制' }))
 expect(await screen.findByText('已复制')).toBeInTheDocument()
})
