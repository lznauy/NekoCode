import { useEffect, useRef, useState } from 'react'
import { safeDeleteTask, safeExportTaskOutputs, safeListTasks, safeSearchTaskOutputs, safeTaskAttempts, safeTaskHistory, safeTaskOutput } from '../lib/wails'
import type { TaskSummary, TaskPage, TaskSearchPage, TaskHistoryPage, TaskAttemptsPage, TaskOutputPage } from '../lib/wails'

export function TaskLibrary({ sessionID }: { sessionID: string }) {
  const [open, setOpen] = useState(false)
  const [all, setAll] = useState(false)
  const [offset, setOffset] = useState(0)
  const [page, setPage] = useState<TaskPage | null>(null)
  const [selected, setSelected] = useState<TaskSummary | null>(null)
  const [history, setHistory] = useState<TaskHistoryPage | null>(null)
  const [attempts, setAttempts] = useState<TaskAttemptsPage | null>(null)
  const [output, setOutput] = useState<{sequence: number; page: TaskOutputPage} | null>(null)
  const [query, setQuery] = useState('')
  const [search, setSearch] = useState<TaskSearchPage | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [refresh, setRefresh] = useState(0)
  const generation = useRef(0)
  const operation = useRef(0)
  const searchRequest = useRef(0)
  useEffect(() => {
    operation.current++
    setSelected(null); setHistory(null); setAttempts(null); setSearch(null); setOutput(null)
    setDeleting(false); setOffset(0); setPage(null); setError(''); setNotice('')
  }, [sessionID, all])
  useEffect(() => {
    const version = ++generation.current
    operation.current++
    if (!open) return
    setPage(null); setError(''); setPending(true)
    void safeListTasks(sessionID, all, offset).then(value => {
      if (generation.current === version) setPage(value)
    }).catch(err => { if (generation.current === version) setError(String(err)) })
      .finally(() => { if (generation.current === version) setPending(false) })
    return () => { generation.current++; operation.current++ }
  }, [sessionID, all, offset, open, refresh])

  // Every selection and asynchronous operation owns a receipt. Earlier work may
  // finish, but its response can never overwrite a later task, query or session.
  const perform = async <T,>(action: () => Promise<T>, apply: (value: T) => void) => {
    const version = generation.current
    const receipt = ++operation.current
    const current = () => generation.current === version && operation.current === receipt
    setPending(true); setError(''); setNotice('')
    try {
      const value = await action()
      if (current()) apply(value)
    } catch (err) { if (current()) setError(String(err)) }
    finally { if (current()) setPending(false) }
  }
  const inspect = (task: TaskSummary) => {
    setSelected(task); setHistory(null); setAttempts(null); setSearch(null); setOutput(null); setDeleting(false)
    void perform(
      () => Promise.all([safeTaskHistory(task.session_id, task.id), safeTaskAttempts(task.session_id, task.id)]),
      ([records, rounds]) => { setHistory(records); setAttempts(rounds) },
    )
  }
  const find = (after=0) => {
    if (!selected || !query.trim()) return
    const receipt=++searchRequest.current
    void perform(() => safeSearchTaskOutputs(selected.session_id, selected.id, query, after), result => {
      if (searchRequest.current===receipt) setSearch(result)
    })
  }
  const exportOutputs = () => {
    if (!selected) return
    void perform(() => safeExportTaskOutputs(selected.session_id, selected.id), path => {
      if (path) setNotice(`已导出到 ${path}`)
    })
  }
  const deleteSelected = () => {
    if (!selected) return
    void perform(() => safeDeleteTask(selected.session_id, selected.id), () => {
      setSelected(null); setDeleting(false); setRefresh(value=>value+1)
    })
  }
  const olderAttempts = () => {
    if (!selected || !attempts) return
    void perform(() => safeTaskAttempts(selected.session_id, selected.id, attempts.before), setAttempts)
  }
  const olderHistory = () => {
    if (!selected || !history) return
    void perform(() => safeTaskHistory(selected.session_id, selected.id, history.before), setHistory)
  }
  const readOutput = (sequence: number, offset=0) => {
    if (!selected) return
    void perform(() => safeTaskOutput(selected.session_id, selected.id, sequence, offset), page => setOutput({sequence, page}))
  }
  return <section aria-label="任务档案" className="space-y-2 border-t border-border pt-2">
    <button type="button" onClick={() => setOpen(!open)} aria-expanded={open}>任务档案 · 查看历史与输出</button>
    {open && <>
      <div className="flex items-center gap-3">
        <label><input type="checkbox" checked={all} onChange={event=>setAll(event.target.checked)} /> 全部会话</label>
        <button type="button" disabled={pending} onClick={()=>setRefresh(value=>value+1)}>刷新</button>
        <span>{page?.total ?? 0} 项任务</span>
      </div>
      <div className="max-h-64 space-y-2 overflow-auto">
        {page?.items?.map(task=><button type="button" key={`${task.session_id}/${task.id}`} onClick={()=>inspect(task)} className="block w-full rounded bg-surface p-2 text-left">
          <span>{task.title || task.id} · {task.status}</span><span className="block text-text-3">会话 {task.session_id} · {task.run_count} 轮</span>
        </button>)}
      </div>
        <div className="flex gap-3"><button type="button" disabled={pending || offset===0} onClick={()=>setOffset(Math.max(0,offset-50))}>上一页</button><button type="button" disabled={pending || !page?.has_more} onClick={()=>setOffset(page?.next_offset ?? 0)}>下一页</button></div>
      {selected && <div className="space-y-2 rounded bg-surface p-3">
        <p>{selected.title || selected.id} · {selected.status}</p>
        {selected?.workspace_kind && <p>工作区：{selected.workspace_kind === 'snapshot' ? '工作区快照' : selected.workspace_kind === 'head' ? 'HEAD 独立副本' : selected.workspace_kind === 'shared' ? '共享工作区' : selected.workspace_kind}</p>}
        <p className="whitespace-pre-wrap">{selected.prompt}</p>
        {selected.result && <pre className="max-h-40 overflow-auto whitespace-pre-wrap">{selected.result}</pre>}
        <div className="flex gap-3">
          <button type="button" disabled={pending} onClick={exportOutputs}>导出输出</button>
          <button type="button" disabled={pending || ['running','queued','waiting_dependencies','waiting_approval','waiting_question'].includes(selected.status)} onClick={()=>setDeleting(true)}>删除任务</button>
        </div>
        {deleting && <div role="alertdialog" aria-label="确认删除任务"><p>删除记录和专属输出？文件修改及 worktree 会保留。</p><button type="button" onClick={()=>setDeleting(false)}>保留</button><button type="button" disabled={pending} onClick={deleteSelected}>确认删除</button></div>}
        <form onSubmit={event=>{event.preventDefault();find()}} className="flex gap-2"><input aria-label="搜索任务输出" value={query} onChange={event=>{searchRequest.current++;setQuery(event.target.value);setSearch(null)}} placeholder="搜索已保存工具输出" className="min-w-0 flex-1 bg-surface-2 p-1"/><button type="submit" disabled={pending || !query.trim()}>搜索</button></form>
        {search && <div><p>{search.hits.length ? '匹配的工具输出' : '没有匹配输出'}</p>{search.hits.map(hit=><pre key={hit.sequence} className="whitespace-pre-wrap">#{hit.sequence} {hit.preview}</pre>)}{search.has_more && <button type="button" disabled={pending} onClick={()=>find(search.next ?? 0)}>更多匹配</button>}</div>}
        <details><summary>执行轮次</summary>{attempts?.attempts?.map(attempt=><p key={attempt.id}>第 {attempt.seq} 轮 · {attempt.status} · {attempt.total_tokens ?? 0} tokens</p>)}{attempts?.has_more && <button type="button" disabled={pending} onClick={olderAttempts}>更早轮次</button>}</details>
        <details><summary>任务过程</summary><div className="max-h-64 overflow-auto">{history?.records?.map((record,index)=><div key={`${record.sequence}/${index}`}><pre className="whitespace-pre-wrap">#{record.sequence} {record.role}: {record.content}</pre>{record.role==='tool' && record.sequence !== undefined && <button type="button" disabled={pending} onClick={()=>readOutput(record.sequence!)}>完整工具输出</button>}</div>)}</div>{history?.has_more && <button type="button" disabled={pending} onClick={olderHistory}>更早消息</button>}</details>
        {output && <div><pre className="max-h-64 overflow-auto whitespace-pre-wrap">{output.page.content}</pre>{output.page.incomplete && <p>输出未完整保存</p>}{output.page.has_more && <button type="button" disabled={pending} onClick={()=>readOutput(output.sequence,output.page.next_offset)}>继续读取</button>}</div>}
      </div>}
      {notice && <p role="status">{notice}</p>}{error && <p role="alert" className="text-danger">{error}</p>}
    </>}
  </section>
}
