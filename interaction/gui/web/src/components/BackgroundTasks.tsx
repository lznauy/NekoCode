import { useCallback, useEffect, useRef, useState } from 'react'
import { safeBackgroundUsage, safeEventsOn, safeRetryTaskReview, safeBackgroundDemands, safeResumeBackgroundDemand, safeDiscardBackgroundDemand, safeAppendBackgroundTask, safeBackgroundTasks, safeCancelBackgroundTask, safeStopBackgroundTasks, safeSubmitBackgroundDemand } from '../lib/wails'
import type { BackgroundTask, BackgroundDemand } from '../lib/wails'

const active = (task: BackgroundTask) => ['queued', 'running', 'waiting_approval', 'waiting_question', 'waiting_dependencies'].includes(task.status)
const labels: Record<string, string> = { waiting_dependencies: '等待上游', blocked: '上游未成功', queued: '排队中', running: '运行中', waiting_approval: '等待审批', waiting_question: '等待回答', completed: '已完成', partial: '部分完成', failed: '失败', interrupted: '已中断' }

export function BackgroundTasks({ sessionID, busy }: { sessionID: string; busy: boolean }) {
  const [totalTokens,setTotalTokens]=useState(0)
  const [tasks, setTasks] = useState<BackgroundTask[]>([])
  const [demands, setDemands] = useState<BackgroundDemand[]>([])
  const [open, setOpen] = useState(false)
  const [target, setTarget] = useState('')
  const [text, setText] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const [loadError, setLoadError] = useState('')
  const [notice, setNotice] = useState('')
  const mounted = useRef(true)
  const request = useRef(0)
  const sessionGeneration = useRef(0)
  const refresh = useCallback(async () => {
    const version = ++request.current
    try {
      const [rows, inbox, usage] = await Promise.all([safeBackgroundTasks(), safeBackgroundDemands(sessionID), safeBackgroundUsage(sessionID)])
      if (mounted.current && version === request.current) { setTotalTokens(usage.total_tokens);setTasks(rows.filter(task => task.session_id === sessionID)); setDemands(inbox.filter(item => item.session_id === sessionID)); setLoadError('') }
    } catch (err) {
      if (mounted.current && version === request.current) setLoadError(String(err))
    }
  }, [sessionID])
  useEffect(() => {
    const generation = ++sessionGeneration.current
    mounted.current = true
    setTasks([]);setTotalTokens(0);setDemands([]);setTarget('');setError('');setNotice('');setPending(false)
    let timer: ReturnType<typeof setTimeout>
    let disposed = false
    const poll = async () => {
      await refresh()
      if (!disposed) timer = setTimeout(poll, 5000)
    }
    void poll()
    let eventTimer: ReturnType<typeof setTimeout>
    const off = safeEventsOn('background:changed', () => {
      clearTimeout(eventTimer)
      eventTimer=setTimeout(()=>void refresh(),150)
    })
    return () => { if (sessionGeneration.current===generation) sessionGeneration.current++;off();clearTimeout(eventTimer);disposed = true; mounted.current = false; request.current++; clearTimeout(timer) }
  }, [refresh])

  const perform = async (operation: () => Promise<void>, message: string) => {
    if (pending) return false
    const generation=sessionGeneration.current
    const current=()=>mounted.current && sessionGeneration.current===generation
    setPending(true); setError(''); setNotice('')
    try {
      await operation()
      if (current()) { setNotice(message); await refresh() }
      return current()
    } catch (err) {
      if (current()) setError(String(err))
      return false
    } finally { if (current()) setPending(false) }
  }
  const selected = tasks.find(task => task.id === target)
  const validTarget = !target || !!selected && active(selected)
  const inbox = demands.filter(item => ['pending', 'interrupted', 'failed', 'queued', 'claimed'].includes(item.status))
  const running = tasks.filter(active).length
  const unread = tasks.reduce((sum, task) => sum + (task.pending_updates?.length ?? 0), 0)
  const submit = async () => {
    const content = text.trim()
    if (!content || !validTarget) return
    const accepted = await perform(
      () => target ? safeAppendBackgroundTask(sessionID, target, content) : safeSubmitBackgroundDemand(sessionID, content),
      target ? '追加已排队，任务将在下一轮请求前接收。' : '新需求已受理，将在协调者空闲时处理。',
    )
    if (accepted && mounted.current) setText('')
  }

  return <section aria-label="后台任务" className="border-t border-border bg-surface-2 px-5 py-2 text-xs">
    <div className="flex items-center justify-between gap-3">
      <button type="button" aria-expanded={open} onClick={() => setOpen(!open)} className="rounded-md px-2 py-1 text-text-2 hover:bg-surface-3 active:scale-95 focus-visible:outline focus-visible:outline-primary">
        {open ? '▾' : '▸'} 后台任务 · {running} 项进行中 · {totalTokens} tokens{busy ? ' · 协调者处理中' : ''}{unread > 0 ? ` · ${unread} 条未读追加` : ''}{inbox.length > 0 ? ` · ${inbox.length} 条需求` : ''}
      </button>
      <div className="flex shrink-0 items-center gap-2">
        <button type="button" onClick={() => { setTarget(''); setOpen(true) }} className="rounded-md px-2 py-1 text-primary hover:bg-surface-3 active:scale-95">新需求</button>
        <button type="button" disabled={pending || !sessionID} onClick={() => void perform(() => safeStopBackgroundTasks(sessionID), '已请求停止协调轮次、后台任务和排队需求。')} className="rounded-md px-2 py-1 text-danger hover:bg-danger/10 active:scale-95 disabled:opacity-40">停止全部</button>
      </div>
    </div>
    {open && <div className="mt-2 space-y-3">
      <div className="max-h-48 space-y-1 overflow-y-auto" aria-label="任务列表">
        {tasks.length === 0 && <p className="px-2 py-1 text-text-3">暂无后台任务。可在这里发起新的独立需求。</p>}
        {tasks.map(task => <div key={task.id} className="rounded-md bg-surface px-3 py-2">
          <div className="flex items-center gap-3">
            <div className="min-w-0 flex-1"><p className="truncate text-text">{task.title || task.id}</p><p className="mt-1 truncate text-text-3" title={task.id}>{labels[task.status] ?? task.status} · {task.profile} · {task.id}{task.pending_updates?.length ? ` · ${task.pending_updates.length} 条未读追加` : ''}</p></div>
            {active(task) && <>
              <button type="button" disabled={pending} onClick={() => setTarget(task.id)} aria-label={`追加到 ${task.title || task.id}`} className="shrink-0 rounded-md px-2 py-1 text-primary hover:bg-surface-3 active:scale-95 disabled:opacity-40">追加</button>
              <button type="button" disabled={pending} onClick={() => void perform(() => safeCancelBackgroundTask(task.session_id, task.id), '已请求停止指定任务。')} aria-label={`停止 ${task.title || task.id}`} className="shrink-0 rounded-md px-2 py-1 text-text-2 hover:text-danger active:scale-95 disabled:opacity-40">停止</button>
            </>}
          </div>
          {task.after?.length ? <p className="mt-1 text-text-3">上游：{task.after.join(', ')}</p> : null}
          {task.usage && <p className="mt-1 text-text-3">{task.usage.total_tokens} tokens · {task.usage.tool_use_count} 次工具调用</p>}
          {task.delivery === 'failed' && <div className="mt-1"><p className="text-danger">验收失败：{task.delivery_error || '请检查原对话后重试'}</p><button type="button" disabled={pending} onClick={()=>void perform(()=>safeRetryTaskReview(task.session_id,task.id),'验收已重新排队，不会重新执行任务。')}>重试验收</button></div>}
          {task.error && <p className="mt-1 break-words text-danger">{task.error}</p>}
        </div>)}
      </div>
      {inbox.length > 0 && <div aria-label="需求收件箱" className="max-h-40 space-y-2 overflow-y-auto">
        {inbox.map(item => <div key={item.id} className="rounded-md bg-surface px-3 py-2">
          <p className="break-words text-text">{item.input.text}</p>
          <p className="mt-1 text-text-3">{{pending: '重启前尚未开始', interrupted: '已中断，请检查对话后发起新需求', failed: '处理失败', queued: '等待协调者', claimed: '协调者已接收'}[item.status]}</p>
          {item.error && <p className="mt-1 break-words text-danger">{item.error}</p>}
          <div className="mt-1 flex gap-3">
            {item.status === 'pending' && <button type="button" disabled={pending} onClick={() => void perform(() => safeResumeBackgroundDemand(sessionID, item.id), '需求已重新排队。')} className="rounded-md px-2 py-1 text-primary active:scale-95 disabled:opacity-40">继续处理</button>}
            {['pending', 'interrupted', 'failed'].includes(item.status) && <button type="button" disabled={pending} onClick={() => void perform(() => safeDiscardBackgroundDemand(sessionID, item.id), '已忽略这条需求记录。')} className="rounded-md px-2 py-1 text-text-2 active:scale-95 disabled:opacity-40">忽略</button>}
          </div>
        </div>)}
      </div>}
      <form onSubmit={event => { event.preventDefault(); void submit() }} className="space-y-2">
        <label className="flex items-center gap-2 text-text-2">发送到
          <select aria-label="发送目标" value={target} disabled={pending} onChange={event => { setTarget(event.target.value); setNotice('') }} className="min-w-0 flex-1 rounded-md border border-border bg-surface px-2 py-1 text-text">
            <option value="">新需求（交给协调者）</option>
            {tasks.filter(task => active(task) || task.id === target).map(task => <option key={task.id} value={task.id}>{task.title || task.id} · {labels[task.status] ?? task.status}</option>)}
            {target && !selected && <option value={target}>任务已不可用 · {target}</option>}
          </select>
        </label>
        <textarea aria-label="任务指示" value={text} disabled={pending} onChange={event => setText(event.target.value)} rows={2} placeholder={target ? '补充这个任务的要求；不会打断正在执行的工具' : '描述新的独立需求；协调者忙碌时会排队'} className="w-full resize-y rounded-md border border-border bg-surface p-2 text-text outline-none focus:border-primary disabled:opacity-50" />
        <div className="flex items-center justify-between gap-3"><span className="text-text-3">{validTarget ? 'Enter 换行，点击按钮发送' : '任务已结束，请选择其他目标；草稿已保留。'}</span><button type="submit" disabled={pending || !text.trim() || !validTarget} className="shrink-0 rounded-md bg-primary px-3 py-1.5 font-semibold text-black active:scale-95 disabled:opacity-40">{pending ? '提交中…' : target ? '追加指示' : '提交新需求'}</button></div>
      </form>
    </div>}
    {notice && <p role="status" className="mt-2 px-2 text-text-2">{notice}</p>}
    {(error || loadError) && <p role="alert" className="mt-2 break-words px-2 text-danger">{error || loadError}</p>}
  </section>
}
