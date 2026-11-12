import { runtime } from '../../../wailsjs/go/models'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { BackgroundTasks } from '../BackgroundTasks'
import { safeBackgroundUsage, safeBackgroundDemands, safeResumeBackgroundDemand, safeDiscardBackgroundDemand, safeAppendBackgroundTask, safeBackgroundTasks, safeCancelBackgroundTask, safeStopBackgroundTasks, safeSubmitBackgroundDemand } from '../../lib/wails'

vi.mock('../TaskLibrary', () => ({TaskLibrary: () => null}))
vi.mock('../../lib/wails', () => ({
 safeBackgroundUsage: vi.fn(), safeEventsOn: vi.fn(()=>()=>{}), safeRetryTaskReview: vi.fn(),
 safeBackgroundDemands: vi.fn(), safeResumeBackgroundDemand: vi.fn(), safeDiscardBackgroundDemand: vi.fn(),
  safeBackgroundTasks: vi.fn(), safeAppendBackgroundTask: vi.fn(), safeCancelBackgroundTask: vi.fn(), safeStopBackgroundTasks: vi.fn(), safeSubmitBackgroundDemand: vi.fn(),
}))
const task = new runtime.BackgroundTask({ id: 'task-A', session_id: 'session', group_id: 'group', title: '修改文件', profile: 'executor', status: 'running', notified: false })
beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(safeBackgroundUsage).mockResolvedValue({total_tokens:42,cache_hit_tokens:30,cache_miss_tokens:12,tool_use_count:3,duration_ms:10})
  vi.mocked(safeBackgroundTasks).mockResolvedValue([task])
  vi.mocked(safeBackgroundDemands).mockResolvedValue([])
 vi.mocked(safeResumeBackgroundDemand).mockResolvedValue(undefined)
 vi.mocked(safeDiscardBackgroundDemand).mockResolvedValue(undefined)
  vi.mocked(safeAppendBackgroundTask).mockResolvedValue(undefined)
  vi.mocked(safeCancelBackgroundTask).mockResolvedValue(undefined)
  vi.mocked(safeStopBackgroundTasks).mockResolvedValue(undefined)
  vi.mocked(safeSubmitBackgroundDemand).mockResolvedValue(undefined)
})
async function openPanel() {
  render(<BackgroundTasks sessionID="session" busy />)
  await screen.findByText(/1 项进行中/)
  fireEvent.click(screen.getByRole('button', { name: /后台任务/ }))
}
describe('BackgroundTasks', () => {
  it('addresses updates and cancellation with the task and session identities', async () => {
    await openPanel()
    fireEvent.click(screen.getByRole('button', { name: '追加到 修改文件' }))
    fireEvent.change(screen.getByRole('textbox', { name: '任务指示' }), { target: { value: '追加要求' } })
    fireEvent.click(screen.getByRole('button', { name: '追加指示' }))
    await waitFor(() => expect(safeAppendBackgroundTask).toHaveBeenCalledWith('session', 'task-A', '追加要求'))
    await waitFor(() => expect(screen.getByRole('textbox', { name: '任务指示' })).toHaveValue(''))
    fireEvent.click(screen.getByRole('button', { name: '停止 修改文件' }))
    await waitFor(() => expect(safeCancelBackgroundTask).toHaveBeenCalledWith('session', 'task-A'))
  })
  it('accepts a new demand while the coordinator is busy and stops the whole session explicitly', async () => {
    await openPanel()
    fireEvent.change(screen.getByRole('textbox', { name: '任务指示' }), { target: { value: '另一个独立需求' } })
    fireEvent.click(screen.getByRole('button', { name: '提交新需求' }))
    await waitFor(() => expect(safeSubmitBackgroundDemand).toHaveBeenCalledWith('session', '另一个独立需求'))
    await waitFor(() => expect(screen.getByRole('button', { name: '停止全部' })).not.toBeDisabled())
    fireEvent.click(screen.getByRole('button', { name: '停止全部' }))
    await waitFor(() => expect(safeStopBackgroundTasks).toHaveBeenCalledWith('session'))
  })
  it('offers explicit recovery only for demands that had not started', async () => {
    vi.mocked(safeBackgroundDemands).mockResolvedValue([
      new runtime.BackgroundDemand({id: 'pending', session_id: 'session', input: {text: 'unstarted demand', source: {kind: 'gui_queue'}}, status: 'pending'}),
      new runtime.BackgroundDemand({id: 'interrupted', session_id: 'session', input: {text: 'inspect first', source: {kind: 'gui_queue'}}, status: 'interrupted'}),
    ])
    await openPanel()
    expect(screen.getByText('unstarted demand')).toBeInTheDocument()
    expect(screen.getAllByRole('button', {name: '继续处理'})).toHaveLength(1)
    fireEvent.click(screen.getByRole('button', {name: '继续处理'}))
    await waitFor(() => expect(safeResumeBackgroundDemand).toHaveBeenCalledWith('session', 'pending'))
    await waitFor(() => expect(screen.getAllByRole('button', {name: '忽略'})[1]).not.toBeDisabled())
    fireEvent.click(screen.getAllByRole('button', {name: '忽略'})[1])
    await waitFor(() => expect(safeDiscardBackgroundDemand).toHaveBeenCalledWith('session', 'interrupted'))
  })
  it('does not let a completed submission from another session clear the new draft', async () => {
    let finish!: () => void
    vi.mocked(safeSubmitBackgroundDemand).mockReturnValue(new Promise<void>(resolve=>{finish=resolve}))
    const view=render(<BackgroundTasks sessionID="session" busy />)
    await screen.findByText(/1 项进行中/)
    fireEvent.click(screen.getByRole('button', {name:/后台任务/}))
    fireEvent.change(screen.getByRole('textbox', {name:'任务指示'}), {target:{value:'original demand'}})
    fireEvent.click(screen.getByRole('button', {name:'提交新需求'}))
    await waitFor(()=>expect(safeSubmitBackgroundDemand).toHaveBeenCalledWith('session','original demand'))
    vi.mocked(safeBackgroundTasks).mockResolvedValue([])
    view.rerender(<BackgroundTasks sessionID="other-session" busy={false} />)
    fireEvent.change(screen.getByRole('textbox', {name:'任务指示'}), {target:{value:'new session draft'}})
    await act(async()=>finish())
    expect(screen.getByRole('textbox', {name:'任务指示'})).toHaveValue('new session draft')
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('keeps the draft when submission fails', async () => {
    vi.mocked(safeSubmitBackgroundDemand).mockRejectedValue(new Error('queue full'))
    await openPanel()
    fireEvent.change(screen.getByRole('textbox', { name: '任务指示' }), { target: { value: '保留草稿' } })
    fireEvent.click(screen.getByRole('button', { name: '提交新需求' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('queue full')
    expect(screen.getByRole('textbox', { name: '任务指示' })).toHaveValue('保留草稿')
  })
})
