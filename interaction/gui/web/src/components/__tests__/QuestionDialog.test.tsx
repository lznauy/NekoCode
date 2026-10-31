import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'
import QuestionDialog from '../QuestionDialog'
const reply = vi.fn()
vi.mock('../../lib/wails', () => ({ safeReplyQuestion: (...args: unknown[]) => reply(...args) }))
beforeEach(() => { reply.mockReset(); reply.mockResolvedValue(undefined) })
const entry = (id: string, label: string) => ({ id, questions: [{ question: `${id}?`, options: [{ label }], custom: true }] })
it('resets selected and custom answers when the question changes', async () => {
 const { rerender } = render(<QuestionDialog entry={entry('old', 'OLD')} onDone={vi.fn()} />)
 fireEvent.change(screen.getByRole('textbox'), { target: { value: 'old custom' } })
 rerender(<QuestionDialog entry={entry('new', 'NEW')} onDone={vi.fn()} />)
 await act(async () => { fireEvent.click(screen.getByRole('button', { name: /回答|Answer/ })) })
 expect(reply).toHaveBeenCalledWith('new', [['NEW']], false)
})
it('waits for acknowledgment and prevents duplicate submissions', async () => {
 let finish!: () => void
 reply.mockReturnValue(new Promise<void>(resolve => { finish = resolve }))
 const done = vi.fn()
 render(<QuestionDialog entry={entry('one', 'YES')} onDone={done} />)
 const button = screen.getByRole('button', { name: /回答|Answer/ })
 fireEvent.click(button); fireEvent.click(button)
 expect(done).not.toHaveBeenCalled()
 expect(reply).toHaveBeenCalledTimes(1)
 await act(async () => finish())
 expect(done).toHaveBeenCalledTimes(1)
})
it('keeps the question available when submitting fails', async () => {
 const failed = Promise.reject(new Error('connection lost')); void failed.catch(() => {})
 reply.mockReturnValue(failed)
 const done = vi.fn()
 render(<QuestionDialog entry={entry('one', 'YES')} onDone={done} />)
 fireEvent.click(screen.getByRole('button', { name: /回答|Answer/ }))
 await waitFor(() => expect(screen.getByRole('alert')).toBeInTheDocument())
 expect(done).not.toHaveBeenCalled()
})
it('does not close a new question when an older submission finishes', async () => {
 let finish!: () => void
 reply.mockReturnValue(new Promise<void>(resolve => { finish = resolve }))
 const done = vi.fn()
 const { rerender } = render(<QuestionDialog entry={entry('old', 'OLD')} onDone={done} />)
 fireEvent.click(screen.getByRole('button', { name: /回答|Answer/ }))
 rerender(<QuestionDialog entry={entry('new', 'NEW')} onDone={done} />)
 await act(async () => finish())
 expect(done).not.toHaveBeenCalled()
 expect(screen.getByText('new?')).toBeInTheDocument()
})
