import { useState, useEffect } from 'react'
import { safeReplyConfirm } from '../lib/wails'
import { isUnifiedDiffContent } from '../lib/diffFormat'
import { UnifiedDiff } from './run/UnifiedDiff'
import type { ApprovalContext } from '../types/events'

export interface ConfirmEntry {
  id: string
  toolName: string
  args: Record<string, unknown>
  preview?: string
  kind?: string
  approval?: ApprovalContext
}

function ConfirmDialog({
  entry,
  onDone,
}: {
  entry: ConfirmEntry
  onDone: () => void
}) {
  const isPermission = isPermissionConfirm(entry)
  // Any prompt except "once"-scope (shell.unknown / process.host) can be
  // remembered as an allow rule by the rule engine.
  const canRemember = permissionScope(entry) !== 'once'

  const options: { label: string; ok: boolean; remember: boolean }[] = [
    { label: '仅本次允许', ok: true, remember: false },
  ]
  if (canRemember) {
    options.push({ label: '始终允许', ok: true, remember: true })
  }
  options.push({ label: '拒绝', ok: false, remember: false })

  const [selected, setSelected] = useState(0)

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        setSelected((s) => (s + 1) % options.length)
      } else if (e.key === 'ArrowUp') {
        e.preventDefault()
        setSelected((s) => (s - 1 + options.length) % options.length)
      } else if (e.key === 'Enter') {
        e.preventDefault()
        const opt = options[selected]
        safeReplyConfirm(entry.id, opt.ok, opt.remember)
        onDone()
      } else if (e.key === 'Escape') {
        e.preventDefault()
        safeReplyConfirm(entry.id, false, false)
        onDone()
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [selected, options.length])

  const handle = (ok: boolean, remember = false) => {
    safeReplyConfirm(entry.id, ok, remember)
    onDone()
  }

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
        role="dialog"
        aria-modal="true"
        aria-labelledby="confirm-title"
        className="flex max-h-[calc(100dvh-32px)] w-full max-w-3xl flex-col overflow-hidden card-radius border border-border/70 bg-surface-2 surface-shadow sm:max-h-[760px]"
      >
        <header className="shrink-0 border-b border-border/45 px-4 py-3">
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
          <p className="min-w-0 text-[12px] text-text-3">
            {footerCopy(entry)}
          </p>
          <div className="flex shrink-0 flex-col gap-2">
            {options.map((opt, i) => (
              <button
                key={opt.label}
                type="button"
                onClick={() => handle(opt.ok, opt.remember)}
                className={`h-9 w-full justify-center text-[13px] ${i === selected ? 'ring-2 ring-primary/60' : ''} ${
                  !opt.ok
                    ? 'secondary-button'
                    : opt.remember
                    ? 'primary-button'
                    : 'secondary-button'
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

function PermissionDetails({ entry, canRemember }: { entry: ConfirmEntry; canRemember: boolean }) {
  const approval = entry.approval
  const rows = [
    ['风险', approval?.risk ?? ''],
    ['原因', approval?.reason !== approval?.risk ? approval?.reason ?? '' : ''],
    ['动态结构', friendlyShellStructures(approval?.structures)],
    ['能力', approval?.capabilities?.join('、') ?? ''],
    ['范围', approval?.scope ?? ''],
    ['工作区', approval?.workspace ?? ''],
    ['网络域名', approval?.outbound_domains != null ? (approval.outbound_domains.join('、') || '禁止联网') : ''],
    ['可写目录', approval?.write_paths?.join('、') ?? ''],
    ['命令类别', permissionText(entry, 'commandClass')],
    ['沙箱', approval?.sandbox ?? ''],
  ].filter(([, value]) => value)

  return (
    <section className="border-t border-border/35 px-4 py-3">
      <div className="mb-2 text-[12px] font-medium text-text-2">审批详情</div>
      <div className="grid gap-2 sm:grid-cols-2">
        {rows.map(([label, value]) => (
          <div key={label} className="min-w-0 rounded-md bg-surface px-2.5 py-2">
            <div className="mb-1 text-[10px] text-text-3">{label}</div>
            <div className="break-words font-mono text-[11px] leading-relaxed text-text-2">{value}</div>
          </div>
        ))}
      </div>
      <p className="mt-2 text-[11px] text-text-3">
        {canRemember ? '选择“始终允许”后，相同项目中的同类请求会自动通过。' : '此权限只支持本次允许，不会写入持久授权。'}
      </p>
    </section>
  )
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
  if (entry.approval?.combined && (entry.approval.capabilities?.length ?? 0) > 0) {
    return permissionScope(entry) === 'project'
      ? '一次决定将同时覆盖命令和上方列出的能力，并可记住到当前项目。'
      : '一次决定将同时覆盖命令和上方列出的临时能力。'
  }
  if ((entry.approval?.structures?.length ?? 0) > 0) {
    return permissionScope(entry) === 'project'
      ? '此决定可记住为该命令的精确授权。'
      : '此动态命令只能允许本次执行。'
  }
  if (isPermissionConfirm(entry)) return permissionScope(entry) === 'project' ? '可选择仅本次允许，或将同类权限记住到当前项目。' : '此权限请求不会持久化，只能本次允许。'
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
