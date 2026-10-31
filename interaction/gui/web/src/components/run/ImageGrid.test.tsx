import { render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { ImageGrid } from './ImageGrid'
vi.mock('../../lib/wails', () => ({ safeReadImageBase64: () => { const p = Promise.reject(new Error('missing')); void p.catch(() => {}); return p } }))
it('shows an unavailable image when reading fails', async () => {
 render(<ImageGrid images={[{ path: '/missing.png', width: 0, height: 0 }]} />)
 expect(await screen.findByText(/图片已丢失/)).toBeInTheDocument()
 expect(screen.queryByText('加载中…')).not.toBeInTheDocument()
})
