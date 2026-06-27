import { safeReplyConfirm } from '../lib/wails'
import { EditDiff } from './run/EditDiff'

export interface ConfirmEntry {
  id: string
  toolName: string
  args: Record<string, unknown>
  preview?: string
  level: number
}

function ConfirmDialog({
  entry,
  onDone,
}: {
  entry: ConfirmEntry
  onDone: () => void
}) {
  const handle = (ok: boolean) => {
    safeReplyConfirm(entry.id, ok)
    onDone()
  }

  const level = riskFor(entry.level)
  const isEdit = entry.toolName === 'edit'
  const isEditRevert = isEdit && entry.args.revert === true
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
        className="flex max-h-[calc(100dvh-32px)] w-full max-w-3xl flex-col overflow-hidden rounded-lg border border-border/70 bg-surface-2 surface-shadow sm:max-h-[760px]"
      >
        <header className="shrink-0 border-b border-border/45 px-4 py-3">
          <div className="flex items-start justify-between gap-3">
            <div className="min-w-0">
              <div className="mb-1 flex items-center gap-2">
                <span className={`rounded px-1.5 py-0.5 text-[10px] font-semibold ${level.className}`}>
                  {level.label}
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
          {isEdit && !isEditRevert ? (
            <div>
              {entry.args.replaceAll === true && (
                <ReplaceAllNotice count={replacementCount} />
              )}
              {entry.preview ? (
                <EditDiff content={entry.preview} filePath={path} defaultCollapsed={false} skipHeader />
              ) : (
                <div className="border-b border-border/30 px-4 py-3 text-[12px] text-warning">
                  未收到 edit diff 预览，将只显示调用参数。
                </div>
              )}
            </div>
          ) : (
            <PrimaryPreview entry={entry} />
          )}

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

        <footer className="flex shrink-0 flex-col gap-2 border-t border-border/45 bg-surface-2 px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:gap-3">
          <p className="min-w-0 text-[12px] text-text-3">
            {footerCopy(entry)}
          </p>
          <div className="flex shrink-0 justify-end gap-2">
            <button
              type="button"
              onClick={() => handle(false)}
              className="secondary-button h-8 px-3"
            >
              拒绝
            </button>
            <button
              type="button"
              onClick={() => handle(true)}
              className="primary-button h-8 px-3"
            >
              允许执行
            </button>
          </div>
        </footer>
      </section>
    </div>
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
  if (entry.toolName === 'bash' && key === 'command') return false
  return true
}

function riskFor(level: number): { label: string; className: string } {
  if (level >= 3) return { label: '禁止', className: 'bg-danger/15 text-danger' }
  if (level >= 2) return { label: '高风险', className: 'bg-warning/15 text-warning' }
  if (level >= 1) return { label: '修改', className: 'bg-primary/15 text-primary' }
  return { label: '安全', className: 'bg-success/15 text-success' }
}

function titleFor(entry: ConfirmEntry): string {
  switch (entry.toolName) {
    case 'edit':
      return '确认文件编辑'
    case 'write':
      return '确认写入文件'
    case 'bash':
      return '确认执行命令'
    default:
      return '确认工具调用'
  }
}

function scopeFor(entry: ConfirmEntry): string {
  if (entry.toolName === 'edit') return 'file edit'
  if (entry.toolName === 'write') return 'file write'
  if (entry.toolName === 'bash') return 'command'
  return 'tool'
}

function subjectFor(entry: ConfirmEntry): string {
  if (typeof entry.args.path === 'string' && entry.args.path) return entry.args.path
  if (typeof entry.args.source === 'string' && entry.args.source) return entry.args.source
  return ''
}

function footerCopy(entry: ConfirmEntry): string {
  if (entry.toolName === 'edit' && entry.args.revert === true) return '允许后将恢复该文件最近一次 edit 前的快照。'
  if (entry.toolName === 'edit' && entry.args.replaceAll === true) return 'replaceAll 会替换所有精确匹配，请确认替换范围。'
  if (entry.toolName === 'edit' && entry.preview) return '上方差异是本次 edit 将应用的内容。'
  if (entry.toolName === 'bash') return '命令会在当前工作区执行。'
  return '允许后工具会继续执行，拒绝会返回 cancelled。'
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
