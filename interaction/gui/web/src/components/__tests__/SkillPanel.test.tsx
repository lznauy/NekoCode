import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'
import { SkillPanel } from '../SkillPanel'
const refresh = vi.fn()
vi.mock('../../lib/wails', () => ({ isWailsEnvironment: () => true, safeSkillManagementView: async () => ({ skills: [{ name: 'existing-skill', loaded: false, source: 'local', sourceKind: 'local' }], plugins: [], mcp: [] }), safeRefreshSkillManagement: () => refresh(), safeSetPluginEnabled: vi.fn() }))
beforeEach(() => { refresh.mockReset() })
it('preserves the extension list when refreshing is rejected', async () => {
 refresh.mockRejectedValue(new Error('runtime busy'))
 render(<SkillPanel open onClose={vi.fn()} />)
 expect(await screen.findByText('existing-skill')).toBeInTheDocument()
 fireEvent.click(screen.getByRole('button', { name: /刷新/ }))
 expect(await screen.findByText('runtime busy')).toBeInTheDocument()
 expect(screen.getByText('existing-skill')).toBeInTheDocument()
})
it('disables refresh while a run is active', async () => {
 render(<SkillPanel open busy onClose={vi.fn()} />)
 await screen.findByText('existing-skill')
 expect(screen.getByRole('button', { name: /刷新/ })).toBeDisabled()
})
