import { PendingPrompts } from './PendingPrompts'
import { useState, useEffect, useRef, useCallback } from 'react'
import { safeReplyConfirm } from '../lib/wails'
import { isUnifiedDiffContent } from '../lib/diffFormat'
import { UnifiedDiff } from './run/UnifiedDiff'
import type { ApprovalContext } from '../types/events'

export interface ConfirmEntry {
  subAgentID?: string
  id: string
  toolName: string
  args: Record<string, unknown>
  preview?: string
  kind?: string
  approval?: ApprovalContext
}

function ConfirmDialog(props: { entry: ConfirmEntry; onDone: () => void; pendingCount?: number; pendingPreview?: string[] }) {
  return <ConfirmDialogRequest key={props.entry.id} {...props} />
}

function ConfirmDialogRequest({
  entry,
  onDone,
  pendingCount = 0,
  pendingPreview = [],
}: {
  entry: ConfirmEntry
  onDone: () => void; pendingCount?: number; pendingPreview?: string[]
}) {
  const isPermission = isPermissionConfirm(entry)
  const suppliedOptions = entry.approval?.options
  const canRemember = suppliedOptions?.length
    ? suppliedOptions.some(option => option.remember)
    : permissionScope(entry) !== 'once'
  const options = suppliedOptions?.length
    ? suppliedOptions.map(option => ({ label: option.label, ok: option.allowed, remember: option.remember }))
    : [
        { label: entry.toolName === 'workspace' ? '本次会话允许' : '仅本次允许', ok: true, remember: false },
        ...(canRemember ? [{ label: '始终允许', ok: true, remember: true }] : []),
        { label: '拒绝', ok: false, remember: false },
      ]

  const [selected, setSelected] = useState(0)
  const [submitting, setSubmitting] = useState(false)
  const [submitError, setSubmitError] = useState('')
  const inFlight = useRef(false)
  const active = useRef(true)
  const dialog = useRef<HTMLElement>(null)

  useEffect(() => {
    active.current = true
    const previous = document.activeElement
    const element = dialog.current
    element?.focus()
    return () => {
      active.current = false
      if (element?.contains(document.activeElement) && previous instanceof HTMLElement) previous.focus()
    }
  }, [])

  const handle = useCallback(async (ok: boolean, remember = false) => {
    if (inFlight.current) return
    inFlight.current = true
    setSubmitting(true)
    setSubmitError('')
    try {
      await safeReplyConfirm(entry.id, ok, remember)
      if (active.current) onDone()
    } catch {
      if (active.current) setSubmitError('提交未成功，请重试或检查请求状态。')
    } finally {
      if (active.current) {
        inFlight.current = false
        setSubmitting(false)
      }
    }
  }, [entry.id, onDone])

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.repeat || e.isComposing || e.ctrlKey || e.altKey || e.metaKey || inFlight.current) return
      if (e.target instanceof HTMLElement && e.target.closest('input, textarea, select, [contenteditable="true"]')) return
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        setSelected((s) => (s + 1) % options.length)
      } else if (e.key === 'ArrowUp') {
        e.preventDefault()
        setSelected((s) => (s - 1 + options.length) % options.length)
      } else if (e.key === 'Enter') {
        if (e.target instanceof HTMLElement && e.target.closest('button')) return
        e.preventDefault()
        const opt = options[selected]
        void handle(opt.ok, opt.remember)
      } else if (e.key === 'Escape') {
        e.preventDefault()
        void handle(false)
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [selected, options.length, handle])

  const kind = kindFor(entry)
  const isEdit = entry.toolName === 'edit'
  const path = typeof entry.args.path === 'string' ? entry.args.path : ''
  const subject = subjectFor(entry)
  const visibleArgs = Object.entries(entry.args).filter(([k]) => showArg(entry, k))
  const hasDetails = visibleArgs.length > 0
  const replacementCount = replacementCountFromPreview(entry.preview)

  return (
    <div className="fixed inset-0 z-[60] flex items-end justify-center bg-black/45 px-3 py-4 backdrop-blur-[2px] sm:items-center">
      <section
        ref={dialog}
        tabIndex={-1}
        role="dialog"
        aria-modal="true"
        aria-labelledby="confirm-title"
        className="flex max-h-[calc(100dvh-32px)] w-full max-w-3xl flex-col overflow-hidden card-radius border border-border/70 bg-surface-2 surface-shadow sm:max-h-[min(760px,calc(100dvh-32px))]"
      >
        <header className="shrink-0 border-b border-border/45 px-4 py-3">
          <PendingPrompts count={pendingCount} previews={pendingPreview} />
          {entry.subAgentID && <p className="mb-2 break-all text-xs text-text-3">子任务：{entry.subAgentID}</p>}
          <div className="flex items-start justify-between gap-3">
            <div className="min-w-0">
              <div className="mb-1 flex items-center gap-2">
                <span className={`rounded px-1.5 py-0.5 text-[10px] font-semibold ${kind.className}`}>
                  {kind.label}
                </span>
                <span className="font-mono text-[12px] text-text-3">{entry.toolName}</span>
              </div>
              <h2 id="confirm-title" className="truncate text-[14px] font-semibold text-text">
                {titleFor(entry)}
              </h2>
            </div>
            <div className="hidden shrink-0 rounded-md bg-surface px-2.5 py-1 text-right sm:block">
              <div className="text-[10px] text-text-3">范围</div>
              <div className="font-mono text-[11px] text-text-2">{scopeFor(entry)}</div>
            </div>
          </div>
          {subject && (
            <div className="mt-2 truncate font-mono text-[12px] text-text-2">
              {subject}
            </div>
          )}
        </header>

        <div className="min-h-0 flex-1 overflow-y-auto">
          {isEdit ? (
            <div>
              {entry.args.replaceAll === true && (
                <ReplaceAllNotice count={replacementCount} />
              )}
              {entry.preview && isUnifiedDiffContent(entry.preview) ? (
                <UnifiedDiff content={entry.preview} filePath={path} defaultCollapsed={false} skipHeader />
              ) : (
                <div className="border-b border-border/30 px-4 py-3 text-[12px] text-warning">
                  未收到 edit diff 预览，将只显示调用参数。
                </div>
              )}
            </div>
          ) : (
            <PrimaryPreview entry={entry} />
          )}

          {isPermission && <PermissionDetails entry={entry} canRemember={canRemember} />}

          {hasDetails && (
            <details className="border-t border-border/35">
              <summary className="cursor-pointer select-none px-4 py-2 text-[12px] font-medium text-text-2 hover:bg-surface-3/40">
                调用参数
              </summary>
              <div className="space-y-2 px-4 pb-3">
                {visibleArgs.map(([k, v]) => (
                  <div key={k}>
                    <div className="mb-1 text-[11px] text-text-3">{k}</div>
                    <pre className="max-h-[160px] overflow-auto whitespace-pre-wrap rounded-md bg-surface px-2.5 py-2 font-mono text-[11px] leading-relaxed text-text-2">
                      {formatValue(v)}
                    </pre>
                  </div>
                ))}
              </div>
            </details>
          )}
        </div>

        <footer className="flex shrink-0 flex-col gap-2 border-t border-border/45 bg-surface-2 px-4 py-3">
          {submitError && <p role="alert" className="text-[12px] text-warning">{submitError}</p>}
          {submitting && <p role="status" className="text-[12px] text-text-2">正在提交决定…</p>}
          {footerCopy(entry) && <p className="min-w-0 text-[12px] text-text-3">{footerCopy(entry)}</p>}
          <div className="flex shrink-0 flex-col gap-2">
            {options.map((opt, i) => (
              <button
                key={opt.label}
                type="button"
                disabled={submitting}
                onClick={() => void handle(opt.ok, opt.remember)}
                className={`h-9 w-full justify-center text-[13px] ${i === selected ? 'ring-2 ring-primary/60' : ''} ${
                  opt.ok && !opt.remember ? 'primary-button' : 'secondary-button'
                }`}
              >
                <span aria-hidden="true">{i === selected ? '▸ ' : '  '}</span>{opt.label}
              </button>
            ))}
          </div>
          <div className="mt-1 border-t border-border/30 pt-2">
            <span className="select-none text-[11px] text-text-3">↑↓ 选择 · Enter 确认 · Esc 拒绝</span>
          </div>
        </footer>
      </section>
    </div>
  )
}

function friendlyPermissionReason(reason?: string): string {
  switch (reason) {
    case 'jev: judgment failed': return 'jev判断失败，请手动决定。'
    case 'jev: judged dangerous': return 'JEV 判定命令存在风险。'
    case 'jev: URL judged risky by the risk judge': return 'JEV 判定网址存在风险。'
    case 'dynamic shell execution': return '命令包含动态执行，需要手动确认。'
    case 'command requires public network access': return '命令需要访问公共网络。'
    default: return reason ?? ''
  }
}

function approvalReason(entry: ConfirmEntry): string {
  const approval = entry.approval
  const reasons: string[] = []
  if (approval?.risk) reasons.push(friendlyPermissionReason(approval.risk))
  if (approval?.reason && approval.reason !== approval.risk) reasons.push(friendlyPermissionReason(approval.reason))
  const source = ({ user_rule: '命中用户设置的询问规则。', builtin_rule: '命中内置询问规则。', default_policy: '当前权限策略需要人工确认。', workspace: '需要扩展工作区访问范围。', capability: '需要授予额外执行权限。' } as Record<string, string>)[approval?.source ?? '']
  if (source && (!reasons.length || approval?.source === 'user_rule' || approval?.source === 'builtin_rule')) reasons.push(source)
  return reasons.join(' ') || '此操作需要你明确授权。'
}

function PermissionDetails({ entry, canRemember }: { entry: ConfirmEntry; canRemember: boolean }) {
  const approval = entry.approval
  const rows = [
    ['触发规则', approval?.rule ?? ''],
    ['动态结构', friendlyShellStructures(approval?.structures)],
    ['能力', approval?.capabilities?.join('、') ?? ''],
    ['授权范围', approval?.scope === 'once' ? '仅本次' : approval?.scope === 'project' ? '当前项目' : approval?.scope ?? ''],
    ['工作区', approval?.workspace ?? ''],
    ['网络域名', approval?.outbound_domains != null ? (approval.outbound_domains.join('、') || '禁止联网') : ''],
    ['可写目录', approval?.write_paths?.join('、') ?? ''],
    ['命令类别', permissionText(entry, 'commandClass')],
    ['沙箱', approval?.sandbox ?? ''],
  ].filter(([, value]) => value)

  return (
    <section className="border-t border-border/35 px-4 py-3">
      <div className="mb-2 text-[12px] font-medium text-text-2">审批详情</div>
      <dl className="mb-3 space-y-2 text-[12px]">
        <div><dt className="text-text-3">确认原因</dt><dd className="mt-1 leading-relaxed text-text-2">{approvalReason(entry)}</dd></div>
      </dl>
      <div className="grid gap-2 sm:grid-cols-2">
        {rows.map(([label, value]) => (
          <div key={label} className="min-w-0 rounded-md bg-surface px-2.5 py-2">
            <div className="mb-1 text-[10px] text-text-3">{label}</div>
            <div className="break-words font-mono text-[11px] leading-relaxed text-text-2">{value}</div>
          </div>
        ))}
      </div>
      <RememberPreview entry={entry} canRemember={canRemember} />
    </section>
  )
}

function RememberPreview({ entry, canRemember }: { entry: ConfirmEntry; canRemember: boolean }) {
  const approval = entry.approval
  if (!canRemember) return <p className="mt-2 text-[11px] text-text-3">仅本次生效，不保存授权。</p>
  const rules = approval?.remember_rules ?? []
  const hasCapabilities = !!approval?.capabilities?.length
  const workspaceRequest = entry.toolName === 'workspace'
  const hasPersistent = hasCapabilities || workspaceRequest || rules.some(rule => rule.lifetime !== 'session')
  return <div className="mt-3 border-t border-border/35 pt-3 text-[11px] text-text-2">
    <p className="font-medium">记住授权的范围</p>
    {hasPersistent && <p className="mt-1 text-text-3">保存到用户授权存储</p>}
    {approval?.remember_workspace && <p className="mt-1 break-all">生效项目：{approval.remember_workspace}</p>}
    {rules.map((rule, i) => <div key={i} className="mt-2 rounded-md bg-surface p-2">
      <p className="text-text-3">{rule.lifetime === 'session' ? '本次会话，仅精确匹配' : rule.literal ? '完整命令，仅精确匹配' : '项目权限匹配规则'}</p>
      <code className="block whitespace-pre-wrap break-all">{rule.literal || `${rule.tool}(${rule.specifier ?? ''})`}</code>
    </div>)}
    {(approval?.approved_rules ?? []).map((rule, i) => <p key={`approved-${i}`} className="mt-1 break-all">已允许：{rule.literal || `${rule.tool}(${rule.specifier ?? ''})`}</p>)}
    {hasCapabilities && <p className="mt-2">同时记住所使用的目录和网络授权范围。</p>}
    {workspaceRequest && <p className="mt-2 break-all">工作区访问：{approval?.workspace || String(entry.args.path ?? '')}（{String(entry.args.access ?? '')}）</p>}
    {!rules.length && !hasCapabilities && !workspaceRequest && <p className="mt-1 text-text-3">未提供规则预览，建议仅本次允许。</p>}
    <p className="mt-2 text-text-3">仅本次允许不保存规则；已有询问、拒绝规则和风险检查仍生效。</p>
  </div>
}

function PrimaryPreview({ entry }: { entry: ConfirmEntry }) {
  const command = typeof entry.args.command === 'string' ? entry.args.command : ''
  const content = typeof entry.preview === 'string' && entry.preview.trim() ? entry.preview : command
  if (!content) {
    return (
      <div className="px-4 py-4 text-[12px] text-text-3">
        此工具没有提供可预览内容。
      </div>
    )
  }
  return (
    <pre className="overflow-x-auto whitespace-pre-wrap px-4 py-3 font-mono text-[12px] leading-relaxed text-text-2">
      {content}
    </pre>
  )
}

function ReplaceAllNotice({ count }: { count: number | null }) {
  const highImpact = count !== null && count > 20
  return (
    <div className={`border-b px-4 py-2 text-[12px] ${
      highImpact
        ? 'border-warning/25 bg-warning/10 text-warning'
        : 'border-primary/20 bg-primary/10 text-text-2'
    }`}>
      replaceAll 将替换{count === null ? '所有精确匹配' : ` ${count} 处精确匹配`}{highImpact ? '，请重点确认范围。' : '。'}
    </div>
  )
}

function showArg(entry: ConfirmEntry, key: string): boolean {
  if (key === '_preview') return false
  if (isPermissionConfirm(entry) && key === 'commandClass') return false
  if (entry.toolName === 'shell' && key === 'command') return false
  return true
}

function kindFor(entry: ConfirmEntry): { label: string; className: string } {
  if (entry.kind === 'install' || entry.toolName === '/plugin install') {
    return { label: '安装', className: 'bg-primary/15 text-primary' }
  }
  return { label: '权限', className: 'bg-warning/15 text-warning' }
}

function titleFor(entry: ConfirmEntry): string {
  switch (entry.toolName) {
    case 'edit':
      return '确认文件编辑'
    case 'write':
      return '确认写入文件'
    case 'shell':
      return '确认执行命令'
    default:
      return '确认工具调用'
  }
}

function scopeFor(entry: ConfirmEntry): string {
  if (isPermissionConfirm(entry)) return permissionScope(entry) || 'permission'
  if (entry.toolName === 'edit') return 'file edit'
  if (entry.toolName === 'write') return 'file write'
  if (entry.toolName === 'shell') return 'command'
  return 'tool'
}

function subjectFor(entry: ConfirmEntry): string {
  if (typeof entry.args.path === 'string' && entry.args.path) return entry.args.path
  if (typeof entry.args.source === 'string' && entry.args.source) return entry.args.source
  return ''
}

function footerCopy(entry: ConfirmEntry): string {
  if (entry.approval?.combined && (entry.approval.capabilities?.length ?? 0) > 0) return '本次决定同时适用于命令和所列权限。'
  if (isPermissionConfirm(entry)) return ''
  if (entry.toolName === 'edit' && entry.args.revert === true) return '上方差异是本次 revert 将恢复的内容。'
  if (entry.toolName === 'edit' && entry.args.replaceAll === true) return 'replaceAll 会替换所有精确匹配，请确认替换范围。'
  if (entry.toolName === 'edit' && entry.preview) return '上方差异是本次 edit 将应用的内容。'
  if (entry.toolName === 'shell') return '命令会在当前工作区执行。'
  return '允许后工具会继续执行，拒绝会返回 cancelled。'
}

function isPermissionConfirm(entry: ConfirmEntry): boolean {
  return entry.approval !== undefined
}

function permissionScope(entry: ConfirmEntry): string {
  return entry.approval?.scope ?? ''
}

function permissionText(entry: ConfirmEntry, key: string): string {
  const value = entry.args[key]
  return typeof value === 'string' ? value : ''
}

function friendlyShellStructures(value: unknown): string {
  if (!Array.isArray(value)) return ''
  const labels: Record<string, string> = {
    command_substitution: '命令替换',
    process_substitution: '进程替换',
    dynamic_command: '动态命令名',
    eval: 'eval 间接执行',
    source: '加载脚本',
    shell_command_string: 'Shell -c 命令字符串',
    shell_heredoc_code: 'Shell heredoc 代码',
    unparseable: '无法可靠解析',
  }
  return value
    .filter((item): item is string => typeof item === 'string')
    .map((item) => labels[item] ?? item)
    .join('、')
}

function replacementCountFromPreview(preview?: string): number | null {
  if (!preview) return null
  const match = preview.match(/\((\d+)\s+replacements?\)/)
  if (!match) return null
  const n = Number(match[1])
  return Number.isFinite(n) ? n : null
}

function formatValue(value: unknown): string {
  if (typeof value === 'string') return value
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value ?? '')
  }
}

export default ConfirmDialog
