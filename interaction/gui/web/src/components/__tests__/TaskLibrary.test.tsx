import { runtime } from '../../../wailsjs/go/models'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { TaskLibrary } from '../TaskLibrary'
import { safeDeleteTask, safeListTasks, safeSearchTaskOutputs, safeTaskAttempts, safeTaskHistory } from '../../lib/wails'
import type { TaskHistoryPage, TaskSummary } from '../../lib/wails'

vi.mock('../../lib/wails', () => ({
  safeListTasks: vi.fn(), safeTaskHistory: vi.fn(), safeTaskAttempts: vi.fn(), safeTaskOutput: vi.fn(),
  safeDeleteTask: vi.fn(), safeExportTaskOutputs: vi.fn(), safeSearchTaskOutputs: vi.fn(),
}))
const task = (id: string): TaskSummary => new runtime.SubagentSummary({ id, session_id: 'session', title: id, profile: 'executor', status: 'completed', prompt: `prompt-${id}`, run_count: 1 })
const history = (content: string): TaskHistoryPage => new runtime.SubagentHistoryPage({ records: [{sequence: 1, role: 'assistant', content}], before: 1, end: 1, total: 1, has_more: false })
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(done => { resolve = done }); return { promise, resolve } }
beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(safeListTasks).mockResolvedValue(new runtime.SubagentPage({session_id: 'session', items: [task('A'), task('B')], total: 2, next_offset: 2, has_more: false}))
  vi.mocked(safeTaskHistory).mockImplementation(async (_parent, id) => history(`history-${id}`))
  vi.mocked(safeTaskAttempts).mockResolvedValue(new runtime.SubagentAttemptPage({session_id: 'session', subagent_id: 'A', attempts: [], before: 0, end: 0, total: 0, has_more: false}))
  vi.mocked(safeDeleteTask).mockResolvedValue(undefined)
})
async function openLibrary() {
  const view = render(<TaskLibrary sessionID="session" />)
  fireEvent.click(screen.getByRole('button', {name: /任务档案/}))
  await screen.findByRole('button', {name: /A · completed/})
  return view
}
describe('TaskLibrary', () => {
  it('keeps the selected task when an earlier detail request finishes last', async () => {
    const first = deferred<TaskHistoryPage>()
    vi.mocked(safeTaskHistory).mockImplementation(async (_parent,id) => id === 'A' ? first.promise : history('history-B'))
    await openLibrary()
    fireEvent.click(screen.getByRole('button', {name: /A · completed/}))
    fireEvent.click(screen.getByRole('button', {name: /B · completed/}))
    await screen.findByText(/history-B/)
    await act(async () => first.resolve(history('history-A')))
    expect(screen.queryByText(/history-A/)).not.toBeInTheDocument()
    expect(screen.getByText(/history-B/)).toBeInTheDocument()
  })
  it('keeps loading task history while the user edits the search query', async () => {
    const records=deferred<TaskHistoryPage>()
    vi.mocked(safeTaskHistory).mockReturnValue(records.promise)
    await openLibrary()
    fireEvent.click(screen.getByRole('button',{name:/A · completed/}))
    fireEvent.change(screen.getByRole('textbox',{name:'搜索任务输出'}),{target:{value:'prepare query'}})
    await act(async()=>records.resolve(history('loaded-history')))
    expect(screen.getByText(/loaded-history/)).toBeInTheDocument()
  })

  it('discards a search response after its query changes', async () => {
    const result = deferred<runtime.TaskSearchPage>()
    vi.mocked(safeSearchTaskOutputs).mockReturnValue(result.promise)
    await openLibrary()
    fireEvent.click(screen.getByRole('button', {name: /A · completed/}))
    await screen.findByText(/history-A/)
    fireEvent.change(screen.getByRole('textbox', {name: '搜索任务输出'}), {target: {value: 'first'}})
    fireEvent.click(screen.getByRole('button', {name: '搜索'}))
    fireEvent.change(screen.getByRole('textbox', {name: '搜索任务输出'}), {target: {value: 'second'}})
    await act(async () => result.resolve(new runtime.TaskSearchPage({hits: [{sequence: 1, preview: 'obsolete-match'}], next: 1, total: 1, has_more: false})))
    expect(screen.queryByText(/obsolete-match/)).not.toBeInTheDocument()
  })
  it('binds delete confirmation to its selected task and does not clear a later selection', async () => {
    const deleted = deferred<void>()
    vi.mocked(safeDeleteTask).mockReturnValue(deleted.promise)
    await openLibrary()
    fireEvent.click(screen.getByRole('button', {name: /A · completed/}))
    await screen.findByText(/history-A/)
    fireEvent.click(screen.getByRole('button', {name: '删除任务'}))
    expect(safeDeleteTask).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', {name: '确认删除'}))
    fireEvent.click(screen.getByRole('button', {name: /B · completed/}))
    await screen.findByText(/history-B/)
    await act(async () => deleted.resolve())
    expect(safeDeleteTask).toHaveBeenCalledWith('session','A')
    expect(screen.getByText('prompt-B')).toBeInTheDocument()
  })
  it('removes old-session rows immediately while loading the selected session', async () => {
    const next = deferred<runtime.SubagentPage>()
    const view=await openLibrary()
    vi.mocked(safeListTasks).mockReturnValue(next.promise)
    view.rerender(<TaskLibrary sessionID="other-session" />)
    await waitFor(() => expect(safeListTasks).toHaveBeenLastCalledWith('other-session',false,0))
    expect(screen.queryByRole('button', {name: /A · completed/})).not.toBeInTheDocument()
    await act(async () => next.resolve(new runtime.SubagentPage({session_id:'other-session',items:[],total:0,next_offset:0,has_more:false})))
  })
})
