import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'
import App from '../App'
const state = vi.hoisted(() => ({ chatError: null as string | null, sessionError: null as string | null, reply: vi.fn(), modeRead: vi.fn(), modeWrite: vi.fn(), listeners: new Map<string, Set<(event: unknown) => void>>() }))
vi.mock('../hooks/useChat', () => ({ useChat: () => ({ msgs: [], text: '', busy: false, error: state.chatError, setText: vi.fn(), send: vi.fn(), stop: vi.fn(), toggleStep: vi.fn(), setMessages: vi.fn(), clearMessages: vi.fn(), imageAttachments: [], pasteImages: vi.fn() }) }))
vi.mock('../hooks/useSessions', async (original) => ({ ...await original<typeof import('../hooks/useSessions')>(), useSessions: () => ({ sessions: [], currentId: null, loading: false, error: state.sessionError, createSession: vi.fn(), switchSession: vi.fn(), deleteSession: vi.fn(), refresh: vi.fn() }) }))
vi.mock('../hooks/useTheme', () => ({ useTheme: () => ({ theme: 'dark', toggle: vi.fn() }) }))
vi.mock('../hooks/useModelInfo', () => ({ useModelInfo: () => '' }))
vi.mock('../hooks/useAutoScroll', () => ({ useAutoScroll: () => ({ containerRef: { current: null }, endRef: { current: null }, follow: vi.fn() }) }))
vi.mock('../lib/wails', async (original) => ({ ...await original<typeof import('../lib/wails')>(), safeInteractionMode: () => state.modeRead(), safeSetInteractionMode: (id: string) => state.modeWrite(id), safeReplyQuestion: (...args: unknown[]) => state.reply(...args), safeGetConfig: async () => null, safeSkillManagementView: async () => null, safeEventsOn: (name: string, cb: (event: unknown) => void) => {
 if (!state.listeners.has(name)) state.listeners.set(name, new Set())
 state.listeners.get(name)!.add(cb)
 return () => { state.listeners.get(name)?.delete(cb) }
} }))
beforeEach(() => { state.chatError = null; state.sessionError = null; state.listeners.clear(); state.modeRead.mockReset().mockResolvedValue(null); state.modeWrite.mockReset(); state.reply.mockReset(); state.reply.mockResolvedValue(undefined) })
const emit = (name: string, payload: unknown) => { for (const cb of state.listeners.get(name) ?? []) cb(payload) }
it.each(['chatError', 'sessionError'] as const)('presents %s to the user', async (kind) => {
 state[kind] = 'operation failed'
 render(<App />)
 expect(screen.getByRole('alert')).toHaveTextContent('operation failed')
 await act(async () => {})
})
it('closes only the matching resolved question', async () => {
 render(<App />)
 act(() => emit('agent:question', { id: 'one', questions: [{ question: 'Continue?', options: [{ label: 'Yes' }] }] }))
 expect(screen.getByRole('dialog')).toBeInTheDocument()
 act(() => emit('agent:question_resolved', { id: 'other' }))
 expect(screen.getByRole('dialog')).toBeInTheDocument()
 act(() => emit('agent:question_resolved', { id: 'one' }))
 await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
})

it('keeps a new question when the old acknowledgment arrives in the same batch', async () => {
 let finish!: () => void
 state.reply.mockReturnValue(new Promise<void>(resolve => { finish = resolve }))
 render(<App />)
 act(() => emit('agent:question', { id: 'old', questions: [{ question: 'Old?', options: [{ label: 'Yes' }] }] }))
 fireEvent.click(screen.getByRole('button', { name: '回答' }))
 await act(async () => {
   emit('agent:question', { id: 'new', questions: [{ question: 'New?', options: [{ label: 'Yes' }] }] })
   finish()
   await Promise.resolve()
 })
 expect(screen.getByText('New?')).toBeInTheDocument()
})

it('keeps the confirmed mode after a failed selection', async () => {
 state.modeRead.mockResolvedValue({selected: 'standard', options: [{id: 'standard', label: 'Standard', description: 'Execute'}, {id: 'plan', label: 'Plan', description: 'Read only'}]})
 state.modeWrite.mockRejectedValue(new Error('runtime busy'))
 render(<App />)
 const select = await screen.findByRole('combobox', {name: '工作模式'})
 fireEvent.change(select, {target: {value: 'plan'}})
 await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('runtime busy'))
 expect(select).toHaveValue('standard')
 await waitFor(() => expect(select).not.toBeDisabled())
})
